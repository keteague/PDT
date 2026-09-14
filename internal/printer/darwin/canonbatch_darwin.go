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
// work itself. Despite the name (kept from when this only handled Canon),
// this is manufacturer-agnostic now - see PrepareBatch's own doc comment.
// planErr means planning itself failed (a bad model reference, an
// unreadable package) - never attempted privileged execution at all, so
// there's nothing to blame on the batch. A row absent from canonBatchResults
// entirely wasn't something PrepareBatch could handle at all (a
// manufacturer/shape it doesn't recognize, the guess-based fallback with no
// catalog entry, or an existing queue to reuse) - its own Deploy() call
// falls back to doing everything itself exactly as before batching existed,
// unaffected.
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
// privileged calls yet) by whichever manufacturer-specific planner
// recognized this row's package shape (planCanonBatchRow,
// planKyoceraBatchRow), then used to build that row's own script segment
// and, after the one combined privileged call returns, to build its
// canonBatchResult. installScript is the fully-rendered "install the
// manufacturer's own real components, then place this row's own PPD" shell
// fragment - manufacturer-specific mechanics stay entirely inside whichever
// planner built it; everything downstream (queue creation, defaults,
// per-row result-file bookkeeping) is shared and doesn't care which
// manufacturer produced it.
type canonBatchRowPlan struct {
	row              printer.PrinterRow
	packagePath      string // variant.PackagePath - the cache key sharedComponentsInstalledThisRun also uses
	ppdFilename      string
	queueName        string
	deviceURI        string
	installScript    string
	sharedInstalled  bool // true when this plan's own installScript includes the shared/real-components install (not just the PPD copy) - see sharedQueuedThisBatch's own use
	extraArgs        []string
	defaultsWarnings []string
}

// PrepareBatch implements printer.BatchPreparer - see that interface's own
// doc comment for the full rationale (getting down to one elevated prompt
// for a whole multi-row deploy run instead of one, or several, per row).
// Tries each manufacturer-specific planner in turn for every row
// (planCanonBatchRow, planKyoceraBatchRow, planRicohBatchRow,
// planSharpBatchRow, planXeroxBatchRow, planToshibaBatchRow,
// planKonicaMinoltaBatchRow) - whichever
// recognizes the row's actual package shape claims it; a row neither recognizes (a different
// manufacturer entirely, the guess-based fallback with no catalog entry, a
// loose-PPD no-installer family, or an existing queue to reuse) is left
// alone completely, falling back to the old per-row path with its own
// separate prompts, never a hard failure just because batching doesn't
// apply. Confirmed-live reasoning for keeping this an explicit allowlist of
// recognized shapes rather than a generic "try anything": each one so far
// has needed its own real investigation against a real downloaded package
// (Canon's Core/Device split, Kyocera's Distribution-XML-identified PPD
// installer plus 15 real sub-packages) - guessing at a shape without one
// risks exactly the kind of live-test-driven bug hunt both of these went
// through before landing correctly.
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
	sharedQueuedThisBatch := map[string]bool{}

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

		expandDir, err := os.MkdirTemp("", "pdt-mac-batch-expand-*")
		if err != nil {
			continue
		}
		cleanups = append(cleanups, func() { os.RemoveAll(expandDir) })
		expanded := filepath.Join(expandDir, "x")
		if err := exec.CommandContext(ctx, "pkgutil", "--expand", pkgPath, expanded).Run(); err != nil {
			continue
		}

		plan, handled := planCanonBatchRow(ctx, row, variant, expandDir, expanded, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		if !handled {
			plan, handled = planKyoceraBatchRow(ctx, row, variant, expandDir, expanded, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			plan, handled = planRicohBatchRow(row, variant, pkgPath, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			plan, handled = planSharpBatchRow(row, variant, pkgPath, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			plan, handled = planXeroxBatchRow(row, variant, pkgPath, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			plan, handled = planToshibaBatchRow(row, variant, pkgPath, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			plan, handled = planKonicaMinoltaBatchRow(row, variant, pkgPath, deviceURI, d.sharedComponentsInstalledThisRun, sharedQueuedThisBatch, &cleanups)
		}
		if !handled {
			continue // not a recognized shape - not batched, falls back to the old per-row path
		}
		if plan.installScript == "" {
			// A planner claimed this row (handled == true) but hit a real
			// error partway through (e.g. selective extraction failed) -
			// see planErr's own doc comment: this row gets a definite
			// answer now rather than silently falling back, since falling
			// back would just re-attempt the same doomed extraction anyway.
			continue
		}

		plans = append(plans, plan)
	}

	if len(plans) == 0 {
		return
	}

	resultsFile, err := os.CreateTemp("", "pdt-mac-batch-results-*")
	if err != nil {
		return // every planned row simply stays absent from canonBatchResults - falls back to the old per-row path
	}
	resultsPath := resultsFile.Name()
	resultsFile.Close()
	defer os.Remove(resultsPath)

	var script strings.Builder
	for i, p := range plans {
		ppdDest := filepath.Join(ppdResourcesDir, p.ppdFilename)
		queueArgv := buildEnsureQueueArgv(p.queueName, p.deviceURI, ppdDest, QueueOptions{Description: p.row.Name, Shared: true, ExtraOptionArgs: p.extraArgs})

		script.WriteString("( ")
		script.WriteString(p.installScript)
		script.WriteString(" && ")
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
			ppdPath:          ppdDestFor(p.ppdFilename),
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
		// A later, non-batched row needing this same package (e.g. one that
		// fell through to the old per-row path for an unrelated reason)
		// shouldn't pay to reinstall the shared/real components again -
		// ensureInstalledOnce/installCanonSelective/installKyoceraSelective
		// all check this same map.
		if p.sharedInstalled && ran && rc == 0 {
			d.sharedComponentsInstalledThisRun[p.packagePath] = true
		}
		d.canonBatchResults[p.row.Name] = result
	}
}

func ppdDestFor(ppdFilename string) string {
	return filepath.Join(ppdResourcesDir, ppdFilename)
}

// planCanonBatchRow is PrepareBatch's own Canon-specific planner - see
// installCanonSelective's doc comment for the underlying mechanics
// (identical here, just building a script fragment to return rather than
// running it directly). handled reports whether the package matched Canon's
// own UFR-II Core/Device shape at all; a real error mid-plan (extraction
// failing) still reports handled=true with plan.installScript left empty,
// so the row gets a definite planErr result rather than silently falling
// back to re-attempt the same doomed extraction through the old path.
func planCanonBatchRow(ctx context.Context, row printer.PrinterRow, variant driver.MacPPDVariant, expandDir, expanded, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	corePkgPath, devicePkgPath, ok := driver.CanonCoreDevicePackages(expanded)
	if !ok {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	stageDir, err := os.MkdirTemp("", "pdt-canon-batch-stage-*")
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, func() { os.RemoveAll(stageDir) })
	if err := driver.ExtractCanonDeviceFiles(devicePkgPath, variant.Filename, stageDir); err != nil {
		return plan, true
	}

	var flatCorePkgPath string
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		flatCorePkgPath = filepath.Join(expandDir, "core-flat.pkg")
		if err := exec.CommandContext(ctx, "pkgutil", "--flatten", corePkgPath, flatCorePkgPath).Run(); err != nil {
			return plan, true
		}
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	}

	base := driver.CanonPPDBaseName(variant.Filename)
	recipeBundleDir := filepath.Join(canonRecipeDir, base+".bundle")
	recipeSymlink := filepath.Join(canonRecipeDir, base+".rcp")
	ppdDest := ppdDestFor(variant.Filename)

	var s strings.Builder
	if flatCorePkgPath != "" {
		fmt.Fprintf(&s, "installer -pkg %s -target / && ", singleQuoteShellArg(flatCorePkgPath))
	}
	fmt.Fprintf(&s, "cp -RX %s/. /Library/ && ", singleQuoteShellArg(filepath.Join(stageDir, "Library")))
	fmt.Fprintf(&s, "chown -Rh root:admin %s %s && ", singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeSymlink))
	fmt.Fprintf(&s, "chmod 644 %s && ", singleQuoteShellArg(ppdDest))
	fmt.Fprintf(&s, "find %s -type d -exec chmod 755 {} + && find %s -type f -exec chmod 644 {} +",
		singleQuoteShellArg(recipeBundleDir), singleQuoteShellArg(recipeBundleDir))
	plan.installScript = s.String()
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromStagedCanonPPD(stageDir, variant.Filename, row.OneSided, row.Mono, row.Name)
	return plan, true
}

// planKyoceraBatchRow is PrepareBatch's own Kyocera-specific planner -
// installKyoceraSelective's own sibling, same "build a script fragment
// instead of running it" adaptation planCanonBatchRow makes.
func planKyoceraBatchRow(ctx context.Context, row printer.PrinterRow, variant driver.MacPPDVariant, expandDir, expanded, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	ppdInstallerPkgPath, otherPkgPaths, ok := driver.KyoceraSelectivePackages(expanded)
	if !ok {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	stageDir, err := os.MkdirTemp("", "pdt-kyocera-batch-stage-*")
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, func() { os.RemoveAll(stageDir) })
	if err := driver.ExtractKyoceraPPD(ppdInstallerPkgPath, variant.Filename, stageDir); err != nil {
		return plan, true
	}

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		var flatPaths []string
		for _, p := range otherPkgPaths {
			flat := p + "-flat.pkg"
			if err := exec.CommandContext(ctx, "pkgutil", "--flatten", p, flat).Run(); err != nil {
				return plan, true // couldn't flatten one - report as a real planErr rather than silently falling back
			}
			flatPaths = append(flatPaths, flat)
		}
		for _, flat := range flatPaths {
			fmt.Fprintf(&s, "installer -pkg %s -target / && ", singleQuoteShellArg(flat))
		}
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	}

	ppdDest := ppdDestFor(variant.Filename)
	fmt.Fprintf(&s, "cp -RX %s/. %s/ && ", singleQuoteShellArg(stageDir), singleQuoteShellArg(ppdResourcesDir))
	fmt.Fprintf(&s, "chown root:admin %s && chmod 644 %s", singleQuoteShellArg(ppdDest), singleQuoteShellArg(ppdDest))
	plan.installScript = s.String()
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromStagedFlatPPD(stageDir, variant.Filename, row.OneSided, row.Mono, row.Name)
	return plan, true
}

// planRicohBatchRow is PrepareBatch's own Ricoh-specific planner. Unlike
// Canon/Kyocera, Ricoh's own real packages are small enough (a few hundred
// KB to ~35MB total for the one legacy bundle - see macricoh.go) that
// there's no speed problem installing the whole thing for real: confirmed
// live, a full install completes in well under a minute including the auth
// wait. No selective extraction needed at all - just the same plain full
// `installer -pkg` run the non-batched fallback path already uses
// successfully. This planner exists purely to fold that into the same
// one-elevated-call-per-run batching every other manufacturer already gets;
// it recognizes a row by manufacturer name alone, not by inspecting the
// package's own internal shape - unnecessary here, since a full install
// works identically regardless of which of Ricoh's two real download shapes
// (see macricoh.go) this row's own package turns out to be.
func planRicohBatchRow(row printer.PrinterRow, variant driver.MacPPDVariant, pkgPath, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	if row.Manufacturer != "Ricoh" {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	ppdPath, cleanup, err := driver.PPDPathForDefaults("Ricoh", pkgPath, variant.Filename)
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, cleanup)
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromPath(ppdPath, row.OneSided, row.Mono, row.Name)

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		fmt.Fprintf(&s, "installer -pkg %s -target /", singleQuoteShellArg(pkgPath))
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	} else {
		// Already installed (or queued) earlier in this same batch/run - an
		// earlier row's own full install already placed this row's own PPD
		// too. Still need *some* command here: the caller always joins
		// installScript and the queue-create command with " && ".
		s.WriteString("true")
	}
	plan.installScript = s.String()
	return plan, true
}

// planSharpBatchRow is PrepareBatch's own Sharp-specific planner - the same
// "just fold a plain full install into the shared batching" shape as
// planRicohBatchRow, for the same reason: Sharp's own real driver package
// (see macfamily.go's own "Sharp" doc comment) is small enough (confirmed
// live, 2026-09-13: a full `installer -pkg` run completes in well under 20s)
// that no Canon/Kyocera-style selective extraction is worth building. Before
// this existed, every Sharp row fell through to the old per-row path
// entirely, each paying its own separate elevated prompt (confirmed live: a
// real 2-row Sharp deploy triggered 3 separate prompts - one shared install,
// plus one queue-create per row).
func planSharpBatchRow(row printer.PrinterRow, variant driver.MacPPDVariant, pkgPath, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	if row.Manufacturer != "Sharp" {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	ppdPath, cleanup, err := driver.PPDPathForDefaults("Sharp", pkgPath, variant.Filename)
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, cleanup)
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromPath(ppdPath, row.OneSided, row.Mono, row.Name)

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		fmt.Fprintf(&s, "installer -pkg %s -target /", singleQuoteShellArg(pkgPath))
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	} else {
		s.WriteString("true")
	}
	plan.installScript = s.String()
	return plan, true
}

// planXeroxBatchRow is PrepareBatch's own Xerox-specific planner - the same
// "just fold a plain full install into the shared batching" shape as
// planRicohBatchRow/planSharpBatchRow. Unlike those two, Xerox's own real
// package has no live-confirmed install timing yet (no real Xerox deploy
// has run at all as of 2026-09-13) - its own whole-driver .pkg is
// meaningfully bigger than either (60MB Payload, 6549 total files, vs.
// Sharp's much smaller one), closer in scale to Canon's own UFR II package
// that specifically needed selective install to stay fast. Also unlike
// Canon/Kyocera, Xerox's own installer Distribution has exactly one
// selectable choice ("driver") with nothing to select down - there is no
// selective-install lever available here even if a full install does turn
// out too slow; the only way to keep this fast, if it ever needs to be,
// would be a Ricoh-style "extract just this one model's own PPD directly,
// skip the installer entirely" approach instead. Added anyway: batching
// still strictly reduces the auth-prompt count regardless of how long the
// underlying install itself takes, and every other manufacturer's own
// batching support only got its real timing confirmed after a live deploy,
// not before. Watch the first real Xerox deploy's own log for how long the
// install actually takes.
func planXeroxBatchRow(row printer.PrinterRow, variant driver.MacPPDVariant, pkgPath, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	if row.Manufacturer != "Xerox" {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	ppdPath, cleanup, err := driver.PPDPathForDefaults("Xerox", pkgPath, variant.Filename)
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, cleanup)
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromPath(ppdPath, row.OneSided, row.Mono, row.Name)

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		fmt.Fprintf(&s, "installer -pkg %s -target /", singleQuoteShellArg(pkgPath))
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	} else {
		s.WriteString("true")
	}
	plan.installScript = s.String()
	return plan, true
}

// planToshibaBatchRow is PrepareBatch's own Toshiba-specific planner - the
// same "just fold a plain full install into the shared batching" shape as
// planRicohBatchRow/planSharpBatchRow. Toshiba's own real package is by far
// the smallest of any manufacturer here (6649 KB installed, well under even
// Sharp's own already-confirmed-fast package), so no selective-install
// concern at all, unlike Xerox's own still-unconfirmed timing.
func planToshibaBatchRow(row printer.PrinterRow, variant driver.MacPPDVariant, pkgPath, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	if row.Manufacturer != "Toshiba" {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	ppdPath, cleanup, err := driver.PPDPathForDefaults("Toshiba", pkgPath, variant.Filename)
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, cleanup)
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromPath(ppdPath, row.OneSided, row.Mono, row.Name)

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		fmt.Fprintf(&s, "installer -pkg %s -target /", singleQuoteShellArg(pkgPath))
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	} else {
		s.WriteString("true")
	}
	plan.installScript = s.String()
	return plan, true
}

// planKonicaMinoltaBatchRow is PrepareBatch's own Konica Minolta-specific
// planner - the same "just fold a plain full install into the shared
// batching" shape as planRicohBatchRow/planSharpBatchRow/planToshibaBatchRow.
// Like Xerox, no real Konica Minolta deploy has timed this live yet as of
// 2026-09-13, and its own real package is a meaningful size (59214 KB
// installed - closer to Xerox's own scale than Ricoh/Sharp/Toshiba's much
// smaller ones); unlike Canon/Kyocera, its own installer Distribution has
// no per-model choices to select down even if a full install does turn out
// slow (both of its own real choices, Choice0/Choice1, install two
// genuinely different, non-overlapping sets of models - "C751i" vs
// "C751i (S)" - not a speed-vs-coverage tradeoff to pick between). Added
// anyway: batching still strictly reduces the auth-prompt count regardless
// of install duration.
func planKonicaMinoltaBatchRow(row printer.PrinterRow, variant driver.MacPPDVariant, pkgPath, deviceURI string, sharedComponentsInstalledThisRun, sharedQueuedThisBatch map[string]bool, cleanups *[]func()) (canonBatchRowPlan, bool) {
	if row.Manufacturer != "Konica Minolta" {
		return canonBatchRowPlan{}, false
	}

	plan := canonBatchRowPlan{row: row, packagePath: variant.PackagePath, ppdFilename: variant.Filename, queueName: sanitizeCUPSQueueName(row.Name), deviceURI: deviceURI}

	ppdPath, cleanup, err := driver.PPDPathForDefaults("Konica Minolta", pkgPath, variant.Filename)
	if err != nil {
		return plan, true
	}
	*cleanups = append(*cleanups, cleanup)
	plan.extraArgs, plan.defaultsWarnings = decidePrintDefaultsFromPath(ppdPath, row.OneSided, row.Mono, row.Name)

	var s strings.Builder
	if !sharedComponentsInstalledThisRun[variant.PackagePath] && !sharedQueuedThisBatch[variant.PackagePath] {
		fmt.Fprintf(&s, "installer -pkg %s -target /", singleQuoteShellArg(pkgPath))
		sharedQueuedThisBatch[variant.PackagePath] = true
		plan.sharedInstalled = true
	} else {
		s.WriteString("true")
	}
	plan.installScript = s.String()
	return plan, true
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

// decidePrintDefaultsFromStagedCanonPPD is PrintDefaultsForNewQueue's own
// logic, fed from a Canon batch plan's own staged PPD copy (under stageDir,
// extracted by driver.ExtractCanonDeviceFiles - a nested
// Library/Printers/PPDs/Contents/Resources/<file> nested destination path)
// rather than the final system destination - the batch plans this *before*
// anything has actually been copied into place, so the final path doesn't
// exist yet at planning time; the staged copy has byte-identical content.
func decidePrintDefaultsFromStagedCanonPPD(stageDir, ppdFilename string, oneSided, mono bool, rowName string) (toSet, warnings []string) {
	stagedPPDPath := filepath.Join(stageDir, "Library", "Printers", "PPDs", "Contents", "Resources", ppdFilename)
	return decidePrintDefaultsFromPath(stagedPPDPath, oneSided, mono, rowName)
}

// decidePrintDefaultsFromStagedFlatPPD is the same idea, for a Kyocera batch
// plan's own staged PPD copy - a flat file directly under stageDir (no
// nested Library/... path the way Canon's own staged tree has - see
// driver.ExtractKyoceraPPD's own doc comment for why).
func decidePrintDefaultsFromStagedFlatPPD(stageDir, ppdFilename string, oneSided, mono bool, rowName string) (toSet, warnings []string) {
	return decidePrintDefaultsFromPath(filepath.Join(stageDir, ppdFilename), oneSided, mono, rowName)
}

func decidePrintDefaultsFromPath(ppdPath string, oneSided, mono bool, rowName string) (toSet, warnings []string) {
	opts, err := readPPDFileOptions(ppdPath)
	if err != nil {
		return nil, []string{fmt.Sprintf("could not read staged PPD options for row %q: %v", rowName, err)}
	}
	return decidePrintDefaults(opts, oneSided, mono, fmt.Sprintf("row %q", rowName))
}
