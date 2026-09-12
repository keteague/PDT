package darwin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"PDT/internal/driver"
	"PDT/internal/printer"
)

// canonBatchResult is PrepareBatch's own stored outcome for one row, read
// back by that row's later Deploy() call instead of doing any privileged
// work itself. planErr means planning itself failed (a bad model reference,
// an unreadable package) - never attempted privileged execution at all, so
// there's nothing to blame on the batch. A row absent from canonBatchResults
// entirely wasn't something PrepareBatch could handle at all (not a
// catalog-driven Canon UFR II row, or already had an existing queue to
// reuse) - its own Deploy() call falls back to doing everything itself
// exactly as before v0.6.9, unaffected by batching.
type canonBatchResult struct {
	planErr          error
	privilegedErr    error
	ppdPath          string
	queueName        string
	deviceURI        string
	defaultsWarnings []string
}

// canonBatchRowPlan is one row's own fully-resolved, not-yet-executed plan -
// built once in PrepareBatch's first pass (all read-only/local work, no
// privileged calls yet), then used to build that row's own script segment
// and, after the one combined privileged call returns, to build its
// canonBatchResult.
type canonBatchRowPlan struct {
	row             printer.PrinterRow
	packagePath     string // variant.PackagePath - the cache key ensureInstalledOnce/canonCoreInstalledThisRun also use
	ppdFilename     string
	queueName       string
	deviceURI       string
	stageDir        string
	flatCorePkgPath string // "" when this package's Core was already covered by an earlier plan in the same batch
	extraArgs       []string
	defaultsWarnings []string
}

// PrepareBatch implements printer.BatchPreparer - see that interface's own
// doc comment for the full rationale. Scoped deliberately narrow: only rows
// that resolve to a catalog-driven Canon UFR II-shaped package (
// driver.MacVariantForDeploy finds an entry, and the package itself expands
// to the expected Core/Device sub-package shape) AND don't already have an
// existing queue to reuse get batched into the one combined elevated call.
// Every other row (a different manufacturer, the guess-based fallback with
// no catalog entry, a loose-PPD no-installer family, or a queue that
// already exists) is left alone entirely - simply absent from
// canonBatchResults afterward, so its own Deploy() call takes the exact same
// path it always has, own separate prompts included. Confirmed-live
// reasoning for this scope: it's the one path proven (repeatedly, this
// session) correct end to end, and every row PDT has actually been
// live-tested with so far falls inside it.
//
// Per-row success/failure comes back through a plain results FILE
// (`printf '%d:%d\n' <rowIndex> $? >> resultsFile`, one line per row's own
// subshell, appended after each row's own commands regardless of whether
// they succeeded), not by parsing the combined call's own stdout - `do
// shell script` was confirmed live (see elevate_darwin.go's own doc
// comment) to mangle \n to \r and to buffer everything until the whole
// script exits, both real problems for structured multi-row output; a
// results file sidesteps both, since Go reads it directly off disk
// afterward instead of parsing anything AppleScript handed back.
func (d *Deployer) PrepareBatch(ctx context.Context, reqs []printer.DeployRequest, confirm printer.Confirm) {
	if d.canonBatchResults == nil {
		d.canonBatchResults = map[string]canonBatchResult{}
	}

	var cleanups []func()
	defer func() {
		for _, c := range cleanups {
			c()
		}
	}()

	var plans []canonBatchRowPlan
	coreQueuedThisBatch := map[string]bool{}

	for _, req := range reqs {
		row := req.Row

		ip, isNul, err := printer.NormalizeIP(row.IP)
		if err != nil || isNul {
			continue // Deploy() reports this same error itself - no privileged work involved either way
		}
		deviceURI := lpdDeviceURI(ip, row.LPDQueueName)

		if _, reused, err := findExistingQueue(ctx, deviceURI); err != nil || reused {
			continue // an existing queue - not something PrepareBatch handles, see its own doc comment
		}

		variant, ok := driver.MacVariantForDeploy(d.ModelIndex, row.Manufacturer, row.Model, row.Driver)
		if !ok || variant.PackagePath == "" {
			continue // guess-based fallback or a loose-PPD family - not batched
		}

		pkgPath, cleanup, err := driver.LocatePkg(variant.PackagePath)
		if err != nil {
			cleanup()
			continue
		}
		cleanups = append(cleanups, cleanup)

		expandDir, err := os.MkdirTemp("", "pdt-canon-batch-expand-*")
		if err != nil {
			continue
		}
		cleanups = append(cleanups, func() { os.RemoveAll(expandDir) })
		expanded := filepath.Join(expandDir, "x")
		if err := exec.CommandContext(ctx, "pkgutil", "--expand", pkgPath, expanded).Run(); err != nil {
			continue
		}

		corePkgPath, devicePkgPath, ok := driver.CanonCoreDevicePackages(expanded)
		if !ok {
			continue // not Canon-UFR-II-shaped - not batched
		}

		stageDir, err := os.MkdirTemp("", "pdt-canon-batch-stage-*")
		if err != nil {
			continue
		}
		cleanups = append(cleanups, func() { os.RemoveAll(stageDir) })
		if err := driver.ExtractCanonDeviceFiles(devicePkgPath, variant.Filename, stageDir); err != nil {
			d.canonBatchResults[row.Name] = canonBatchResult{planErr: fmt.Errorf("selectively extracting %s: %w", variant.Filename, err)}
			continue
		}

		plan := canonBatchRowPlan{
			row:         row,
			packagePath: variant.PackagePath,
			ppdFilename: variant.Filename,
			queueName:   sanitizeCUPSQueueName(row.Name),
			deviceURI:   deviceURI,
			stageDir:    stageDir,
		}

		if !d.canonCoreInstalledThisRun[variant.PackagePath] && !coreQueuedThisBatch[variant.PackagePath] {
			flatCorePkgPath := filepath.Join(expandDir, "core-flat.pkg")
			if err := exec.CommandContext(ctx, "pkgutil", "--flatten", corePkgPath, flatCorePkgPath).Run(); err != nil {
				continue // couldn't even flatten Core - leave this row unbatched, its own Deploy() call retries the old way
			}
			plan.flatCorePkgPath = flatCorePkgPath
			coreQueuedThisBatch[variant.PackagePath] = true
		}

		plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromStagedPPD(stageDir, variant.Filename, row.OneSided, row.Mono, row.Name)

		plans = append(plans, plan)
	}

	if len(plans) == 0 {
		return
	}

	resultsFile, err := os.CreateTemp("", "pdt-canon-batch-results-*")
	if err != nil {
		return // every planned row simply stays absent from canonBatchResults - falls back to the old per-row path
	}
	resultsPath := resultsFile.Name()
	resultsFile.Close()
	defer os.Remove(resultsPath)

	var script strings.Builder
	for i, p := range plans {
		base := driver.CanonPPDBaseName(p.ppdFilename)
		recipeBundleDir := filepath.Join(canonRecipeDir, base+".bundle")
		recipeSymlink := filepath.Join(canonRecipeDir, base+".rcp")
		ppdDest := filepath.Join(ppdResourcesDir, p.ppdFilename)
		queueArgv := buildEnsureQueueArgv(p.queueName, p.deviceURI, ppdDest, QueueOptions{Description: p.row.Name, Shared: true, ExtraOptionArgs: p.extraArgs})

		script.WriteString("( ")
		if p.flatCorePkgPath != "" {
			fmt.Fprintf(&script, "installer -pkg %s -target / && ", singleQuoteShellArg(p.flatCorePkgPath))
		}
		fmt.Fprintf(&script, "cp -RX %s/. /Library/ && ", singleQuoteShellArg(filepath.Join(p.stageDir, "Library")))
		fmt.Fprintf(&script, "chown -Rh root:admin %s %s && ", singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeSymlink))
		fmt.Fprintf(&script, "chmod 644 %s && ", singleQuoteShellArg(ppdDest))
		fmt.Fprintf(&script, "find %s -type d -exec chmod 755 {} + && find %s -type f -exec chmod 644 {} + && ",
			singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeBundleDir))
		script.WriteString(quoteShellCommand(queueArgv))
		fmt.Fprintf(&script, " ) ; printf '%%d:%%d\\n' %d $? >> %s ; ", i, singleQuoteShellArg(resultsPath))
	}

	_, runErr := runPrivilegedShell(ctx, script.String())

	rowRC := map[int]int{}
	if data, readErr := os.ReadFile(resultsPath); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			idx, err1 := strconv.Atoi(parts[0])
			rc, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				rowRC[idx] = rc
			}
		}
	}

	for i, p := range plans {
		rc, ran := rowRC[i]
		result := canonBatchResult{
			ppdPath:          filepath.Join(ppdResourcesDir, p.ppdFilename),
			queueName:        p.queueName,
			deviceURI:        p.deviceURI,
			defaultsWarnings: p.defaultsWarnings,
		}
		switch {
		case !ran:
			// The combined call itself never got far enough to run this
			// row's own subshell at all - e.g. the one auth prompt was
			// declined. runErr (if any) is the most useful thing to report.
			if runErr != nil {
				result.privilegedErr = fmt.Errorf("batched install/queue-create was not completed: %w", runErr)
			} else {
				result.privilegedErr = fmt.Errorf("batched install/queue-create did not run for this row (unknown reason)")
			}
		case rc != 0:
			result.privilegedErr = fmt.Errorf("batched install/queue-create failed (exit %d)", rc)
		}
		if p.flatCorePkgPath != "" && ran && rc == 0 {
			d.canonCoreInstalledThisRun[p.packagePath] = true
		}
		d.canonBatchResults[p.row.Name] = result
	}
}

// deployFromBatchResult builds row's own DeployResult straight from
// PrepareBatch's already-executed outcome - no privileged calls, no
// re-resolving, just formatting the same shape of log lines Deploy()'s own
// normal (non-batched) path would have produced, so the deploy log reads the
// same way either way.
func (d *Deployer) deployFromBatchResult(row printer.PrinterRow, result canonBatchResult, log *printer.Logger) printer.DeployResult {
	if result.planErr != nil {
		log.Err("resolving driver: %v", result.planErr)
		return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: result.planErr}
	}
	if result.privilegedErr != nil {
		log.Err("%v", result.privilegedErr)
		return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: result.privilegedErr}
	}
	log.OK("Installed %q (registered %q).", row.Manufacturer, result.ppdPath)
	if result.queueName != row.Name {
		log.Warn("CUPS queue names can't contain spaces/tabs/\"/\"/\"#\" (unlike a Windows printer object name) - using %q for the actual queue name; %q is still this row's own display name and the queue's own -D description.", result.queueName, row.Name)
	}
	log.OK("Configured queue %q (%s).", result.queueName, result.deviceURI)
	for _, w := range result.defaultsWarnings {
		log.Warn("%s", w)
	}
	log.OK("Deployment finished for %q.", row.Name)
	return printer.DeployResult{RowName: row.Name, Log: log.Lines(), Err: nil}
}

// decidePrintDefaultsFromStagedPPD is PrintDefaultsForNewQueue's own logic,
// fed from a batch plan's own staged PPD copy (under stageDir, extracted by
// driver.ExtractCanonDeviceFiles) rather than the final system destination -
// the batch plans this *before* anything has actually been copied into
// place, so the final path doesn't exist yet at planning time; the staged
// copy has byte-identical content.
func decidePrintDefaultsFromStagedPPD(stageDir, ppdFilename string, oneSided, mono bool, rowName string) (toSet, warnings []string) {
	stagedPPDPath := filepath.Join(stageDir, "Library", "Printers", "PPDs", "Contents", "Resources", ppdFilename)
	opts, err := readPPDFileOptions(stagedPPDPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("could not read staged PPD options for row %q: %v", rowName, err)}
	}
	return decidePrintDefaults(opts, oneSided, mono, fmt.Sprintf("row %q", rowName))
}
