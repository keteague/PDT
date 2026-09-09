package darwin

import (
	"context"
	"fmt"

	"PDT/internal/driver"
	"PDT/internal/printer"
)

// Deployer implements printer.Deployer against a real CUPS installation,
// using the lpadmin/installer/hdiutil-driven bindings in this package.
// Catalog is built once (driver.BuildMacCatalog) and shared across every row
// in a run, the same lifecycle as internal/printer/windows.Deployer.Catalog.
type Deployer struct {
	Catalog driver.MacCatalog
}

func NewDeployer(catalog driver.MacCatalog) *Deployer {
	return &Deployer{Catalog: catalog}
}

// Deploy ports deploy_windows.go's Deployer.Deploy to CUPS/LPD: resolve
// driver -> ensure it's installed -> resolve/create the LPD queue -> best-
// effort print defaults. Two Windows-only steps have no analog here and are
// simply absent, not silently dropped:
//
//   - No NUL:-port-then-rebind workaround. That existed because creating a
//     Windows printer object directly against a live TCP/IP port was
//     observed to take several minutes for some driver families (HP's
//     Universal Print Driver, any Kyocera driver) - see
//     printer.RequiresNulPortWorkaround. CUPS queue creation (lpadmin) never
//     touches the network at creation time at all; there is nothing here for
//     that workaround to apply to.
//   - No APF ("Enable advanced printing features")/"Print spooled documents
//     first". Both are PRINTER_ATTRIBUTE_* bits on a Windows spooler print
//     queue object - there is no CUPS equivalent to set.
//
// Existing-queue handling mirrors deploy_windows.go's diff-then-Confirm flow:
// if a queue named row.Name already exists, its device-uri is compared
// against what this row would set (the only field QueueInfo can reliably
// diff - see its own doc comment for why the PPD isn't compared the same
// way), listing the actual difference in the confirmation prompt via the
// same printer.Confirm callback type.
func (d *Deployer) Deploy(ctx context.Context, req printer.DeployRequest, confirm printer.Confirm) printer.DeployResult {
	log := &printer.Logger{}
	row := req.Row
	fatal := func(err error) printer.DeployResult {
		log.Err("%v", err)
		return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: err}
	}

	log.Info("Starting deployment for %q", row.Name)

	ip, isNul, err := printer.NormalizeIP(row.IP)
	if err != nil {
		return fatal(err)
	}
	if isNul {
		return fatal(fmt.Errorf("%q is not a meaningful IP for an LPD queue on macOS - there is no local NUL:-equivalent placeholder queue here", row.IP))
	}

	ppdPath, err := d.resolveDriver(ctx, row, log)
	if err != nil {
		return fatal(fmt.Errorf("resolving driver: %w", err))
	}

	deviceURI := "lpd://" + ip + "/"

	// Reuse whatever queue (under any name) already targets this exact
	// device, the same "reuse rather than ever create a duplicate" rule
	// ensureRealPort applies to Standard TCP/IP ports on Windows - if found,
	// there is nothing left to decide: no rename, no re-confirmation, just
	// re-apply print defaults below.
	queueName, reusedExisting, err := findExistingQueue(ctx, deviceURI)
	if err != nil {
		return fatal(fmt.Errorf("checking for an existing queue targeting %s: %w", ip, err))
	}

	if reusedExisting {
		log.Info("Reusing existing CUPS queue %q already configured for %s.", queueName, ip)
	} else {
		queueName = row.Name
		// No queue targets this device yet - but row.Name itself might
		// already name a *different* queue (pointed at some other device).
		// Mirrors deploy_windows.go's existing-printer diff: list what would
		// change and confirm before overwriting it.
		if existing, nameTaken := currentQueueInfo(ctx, queueName); nameTaken {
			ok, cerr := confirm(ctx, "Confirm queue update", fmt.Sprintf(
				"A CUPS queue named %q already exists, currently targeting %q.\n\nThis row would repoint it to %q.\n\nApply this change?",
				queueName, existing.DeviceURI, deviceURI))
			if cerr != nil {
				return fatal(fmt.Errorf("confirming update to queue %q: %w", queueName, cerr))
			}
			if !ok {
				log.Warn("Queue %q already exists targeting %q; user declined to repoint it to %q. Leaving it as-is.", queueName, existing.DeviceURI, deviceURI)
				log.OK("Deployment finished for %q.", row.Name)
				return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: nil}
			}
		}
		if err := EnsureQueue(ctx, queueName, deviceURI, ppdPath, QueueOptions{Description: row.Name, Shared: true}); err != nil {
			return fatal(err)
		}
		log.OK("Configured queue %q (%s).", queueName, deviceURI)
	}

	for _, w := range SetPrintDefaults(ctx, queueName, row.OneSided, row.Mono) {
		log.Warn("%s", w)
	}

	log.OK("Deployment finished for %q.", row.Name)
	return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: nil}
}

// resolveDriver ports ensureDriverCurrent's role: resolve row's manufacturer
// to a local package (or an OpenPrinting fallback PPD), installing it if it's
// a package, and returns the PPD path EnsureQueue should use ("" for the
// -m everywhere fallback - see EnsureDriverInstalled's own doc comment).
//
// Unlike Windows, there's no cheap "read the currently-installed version"
// check to skip a redundant reinstall (see PackageLabel's own doc comment for
// why macOS installer packages don't expose one) - every row with a resolved
// package re-runs `installer`, which is itself idempotent (reinstalling the
// same package is a no-op from CUPS' point of view, just a few extra seconds
// per row rather than the multi-minute cost the NUL: workaround existed
// for on Windows).
func (d *Deployer) resolveDriver(ctx context.Context, row printer.PrinterRow, log *printer.Logger) (ppdPath string, err error) {
	if resolved := driver.ResolveMac(d.Catalog, row.Manufacturer); resolved != nil {
		log.Info("Resolved package %q (%s) for %s.", resolved.Path, resolved.Label, row.Manufacturer)
		newPPDs, err := EnsureDriverInstalled(ctx, resolved)
		if err != nil {
			return "", fmt.Errorf("installing %s: %w", resolved.Path, err)
		}
		if len(newPPDs) == 0 {
			log.Warn("Installing %q registered no new PPD under %s - falling back to IPP-Everywhere autoconfiguration.", resolved.Path, ppdResourcesDir)
			return "", nil
		}
		log.OK("Installed %q (registered %d PPD(s)).", resolved.Path, len(newPPDs))
		return choosePPD(newPPDs, row.Driver), nil
	}

	if path, ok := driver.ResolveOpenPrintingPPD(d.Catalog, row.Manufacturer, row.Driver); ok {
		log.Info("No local installer package for %s; using OpenPrinting fallback PPD %q.", row.Manufacturer, path)
		return path, nil
	}

	return "", fmt.Errorf("no usable driver package or fallback PPD found locally for manufacturer %q, driver selection %q", row.Manufacturer, row.Driver)
}

// choosePPD picks the best of an install's newly-registered PPDs for the
// row's own Driver field (free text, typically a model name or fragment of
// one - the same field a fuzzy-matched OpenPrinting PPD label would have
// come from too, see App.DriverCandidates on darwin) - most driver packages
// register several PPDs (one per supported model in the family, as seen
// installing the real Kyocera package: a single install adds one PPD per
// model it supports), fuzzy-matched against each PPD's own filename (the
// same substring/subsequence ranking FuzzyMatchScore already provides),
// falling back to the first one when driverSelection is blank or matches
// nothing (still a usable PPD, just not guaranteed to be the exact model's
// own).
func choosePPD(candidates []string, driverSelection string) string {
	if driverSelection == "" {
		return candidates[0]
	}
	best := candidates[0]
	bestScore := -1
	for _, c := range candidates {
		if score := driver.FuzzyMatchScore(c, driverSelection); score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}
