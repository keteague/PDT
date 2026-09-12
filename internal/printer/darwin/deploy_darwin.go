package darwin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"PDT/internal/driver"
	"PDT/internal/printer"
)

// Deployer implements printer.Deployer against a real CUPS installation,
// using the lpadmin/installer/hdiutil-driven bindings in this package.
// Catalog and ModelIndex are both built once (driver.BuildMacCatalog,
// driver.BuildMacModelIndex) and shared across every row in a run, the same
// lifecycle as internal/printer/windows.Deployer.Catalog.
type Deployer struct {
	Catalog    driver.MacCatalog
	ModelIndex driver.MacModelIndex
}

func NewDeployer(catalog driver.MacCatalog, modelIndex driver.MacModelIndex) *Deployer {
	return &Deployer{Catalog: catalog, ModelIndex: modelIndex}
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

	deviceURI := lpdDeviceURI(ip, row.LPDQueueName)

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

// lpdDeviceURI builds the LPD device URI Deploy hands to EnsureQueue.
// queueName is row.LPDQueueName - blank for every manufacturer PDT knows of
// except HP ("raw") and Xerox ("lp"), see PrinterRow's own doc comment for
// why those two specifically, and frontend/src/main.js's defaultLpdQueueFor
// for where that default actually gets set (still a per-row override, never
// enforced here). Trimmed of stray slashes/whitespace so a queue name typed
// as "/raw" or "raw/" doesn't produce a doubled or trailing slash in the URI.
func lpdDeviceURI(ip, queueName string) string {
	return "lpd://" + ip + "/" + strings.Trim(strings.TrimSpace(queueName), "/")
}

// resolveDriver ports ensureDriverCurrent's role: resolve row's manufacturer
// (and, for a manufacturer that ships more than one distinct driver family -
// see driver.ResolveMacFamily - row's own Model) to a local package or an
// OpenPrinting fallback PPD, installing it if it's a package, and returns
// the PPD path EnsureQueue should use ("" for the -m everywhere fallback -
// see EnsureDriverInstalled's own doc comment).
//
// The build-once model index (driver.MacModelIndex, populated only for a
// manufacturer macFamilyPreference lists - Canon today) is checked first:
// when row.Model resolves to a real entry, driver.MacVariantForDeploy
// already knows exactly which PPD this deploy needs - which package to
// install and which filename it registers, or which permanently-cached PPD
// to use directly for a no-installer family - straight from a catalog built
// once at startup/Refresh, with no per-deploy package re-inspection at all
// (see installVariant). Only when Model doesn't resolve to any index entry
// (manufacturer not in macFamilyPreference, Model left blank, or a typo that
// doesn't fold-match any known model) does this fall back to the older
// guess-based path: driver.ResolveMacFamily picks *a* package to install,
// then choosePPD fuzzy-matches Model against whatever PPDs that install
// actually registers, logging a [WARN] if the pick wasn't a clear winner.
//
// row.Driver, when it holds an *exact* OpenPrintingCandidates label (the
// technician explicitly picked one from the Driver dropdown rather than just
// typing a Model and moving on), is preferred over re-deriving a guess from
// Model in the OpenPrinting-fallback branch - a real selection beats a best
// guess, the same rule MacVariantForDeploy itself applies to row.Driver
// first when the model index has an entry.
//
// Unlike Windows, there's no cheap "read the currently-installed version"
// check to skip a redundant reinstall (see PackageLabel's own doc comment for
// why macOS installer packages don't expose one) - every row with a resolved
// package re-runs `installer`, which is itself idempotent (reinstalling the
// same package is a no-op from CUPS' point of view, just a few extra seconds
// per row rather than the multi-minute cost the NUL: workaround existed
// for on Windows).
func (d *Deployer) resolveDriver(ctx context.Context, row printer.PrinterRow, log *printer.Logger) (ppdPath string, err error) {
	if variant, ok := driver.MacVariantForDeploy(d.ModelIndex, row.Manufacturer, row.Model, row.Driver); ok {
		return d.installVariant(ctx, variant, row, log)
	}

	if resolved, note := driver.ResolveMacFamily(d.Catalog, row.Manufacturer, row.Model); resolved != nil {
		if note != "" {
			log.Warn("%s", note)
		}
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
		chosen, ambiguous := choosePPD(newPPDs, row.Model)
		if ambiguous {
			log.Warn("Model %q did not clearly identify one PPD among the %d this install registered - best guess: %q. Set this row's Model to the printer's real model if that's wrong.", row.Model, len(newPPDs), chosen)
		} else {
			log.Info("Selected PPD %q for model %q.", chosen, row.Model)
		}
		return chosen, nil
	}

	if row.Driver != "" {
		if path, ok := driver.OpenPrintingPPDByLabel(d.Catalog, row.Manufacturer, row.Driver); ok {
			log.Info("Using explicitly-selected OpenPrinting PPD %q for %s.", row.Driver, row.Manufacturer)
			return path, nil
		}
	}
	if path, ok := driver.ResolveOpenPrintingPPD(d.Catalog, row.Manufacturer, row.Model); ok {
		log.Info("No local installer package for %s; matched OpenPrinting fallback PPD %q from model %q.", row.Manufacturer, path, row.Model)
		return path, nil
	}

	return "", fmt.Errorf("no usable driver package or fallback PPD found locally for manufacturer %q (model %q, driver selection %q)", row.Manufacturer, row.Model, row.Driver)
}

// installVariant deploys a driver.MacPPDVariant resolved straight from the
// manufacturer's own build-once model index - no post-install diffing or
// NickName re-matching needed at all, since the index already recorded
// exactly which PPD filename this variant registers (an installer-backed
// family - PackagePath set) or which permanent local copy to use directly (a
// loose, no-installer family - LooseCachedPPDPath set instead, see
// MacPPDVariant's own doc comment for why Canon's own "PPD" bucket needs no
// install step at all: its PPDs declare no *cupsFilter, so there's no vendor
// filter binary an installer would even need to deposit).
func (d *Deployer) installVariant(ctx context.Context, variant driver.MacPPDVariant, row printer.PrinterRow, log *printer.Logger) (string, error) {
	if variant.PackagePath == "" {
		if variant.LooseCachedPPDPath == "" {
			return "", fmt.Errorf("catalog entry for %q (%s) has neither a package nor a cached PPD to use - try Refresh Drivers", variant.NickName, variant.Language)
		}
		log.Info("Using cached %s PPD %q for %s (no installer needed for this family).", variant.Language, variant.NickName, row.Manufacturer)
		return variant.LooseCachedPPDPath, nil
	}

	resolved := &driver.ResolvedMacPackage{Path: variant.PackagePath, Label: variant.NickName}
	log.Info("Installing %s package %q for %q (%s).", variant.Language, variant.PackagePath, variant.NickName, row.Manufacturer)
	if _, err := EnsureDriverInstalled(ctx, resolved); err != nil {
		return "", fmt.Errorf("installing %s: %w", variant.PackagePath, err)
	}

	ppdPath := filepath.Join(ppdResourcesDir, variant.Filename)
	if _, err := os.Stat(ppdPath); err != nil {
		log.Warn("Expected %q to register %q, but it's not there after install (%v) - the vendor package may have changed since this catalog was built. Try Refresh Drivers. Falling back to IPP-Everywhere autoconfiguration.", variant.PackagePath, ppdPath, err)
		return "", nil
	}
	log.OK("Installed %q (registered %q).", variant.PackagePath, ppdPath)
	return ppdPath, nil
}

// choosePPD picks the best of an install's newly-registered PPDs for model -
// most driver packages register several (one per supported model in the
// family, as seen installing the real Kyocera package: a single install adds
// one PPD per model it supports). Matches against each PPD's own *NickName
// (driver.ReadPPDNickName), not its filename - confirmed necessary against a
// real Canon install: Canon's PPD filenames are cryptic codes
// ("CNPZUIRAC5840ZU.ppd.gz") sharing no matchable substring, or even in-order
// character sequence, with how a technician would actually type the model
// ("iR-ADV C5840") - FuzzyMatchScore's subsequence fallback specifically
// fails on the "-" and " " characters the filename never contains, so
// filename matching isn't just weaker here, it's a hard zero. Falls back to
// the bare filename only when a PPD has no readable NickName at all (rare -
// every real PPD inspected so far has one).
//
// ambiguous reports whether the pick was actually confident: a blank model,
// or two-or-more candidates tying for the best score, both mean there wasn't
// enough signal to be sure - the caller logs a [WARN] rather than silently
// guessing wrong with no indication, the gap the README's own "Model-driven
// PPD selection on macOS" section calls out as still unbuilt (a full
// interactive disambiguation prompt remains a further possible enhancement,
// not attempted here).
func choosePPD(candidates []string, model string) (chosen string, ambiguous bool) {
	if len(candidates) == 1 {
		return candidates[0], false
	}
	if model == "" {
		return candidates[0], true
	}
	bestIdx, bestScore, secondScore := 0, -1, -1
	for i, c := range candidates {
		label := c
		if nick, ok := driver.ReadPPDNickName(c); ok {
			label = nick
		}
		score := driver.FuzzyMatchScore(label, model)
		if score > bestScore {
			bestIdx, secondScore, bestScore = i, bestScore, score
		} else if score > secondScore {
			secondScore = score
		}
	}
	if bestScore < 0 {
		return candidates[0], true
	}
	return candidates[bestIdx], bestScore == secondScore
}
