package main

import (
	"fmt"
	"os"
	"path/filepath"

	"PDT/internal/driver"
	"PDT/internal/flashdrive"
	"PDT/internal/printer"
	pdtwin "PDT/internal/printer/windows"
	"PDT/internal/update"
)

// platformStartup runs once at startup before the driver catalog is scanned:
// extracts the bundled 7-Zip tools (BuildCatalog's own auto-extraction of
// self-extracting driver archives depends on it - see sevenzip_windows.go),
// best-effort cleans up a previous self-update's renamed-aside .old exe (see
// internal/update.Apply - by the time this process is running at all,
// whatever process left that file behind has necessarily already exited),
// and scaffolds the standard Drivers\Windows\11\<Manufacturer> folders for a
// brand-new install (ensureDriversScaffold is already unconditional/
// idempotent - see its own doc comment).
func (a *App) platformStartup() {
	if exe, err := os.Executable(); err == nil {
		update.CleanupOldExe(exe)
	}
	ensureSevenZipExtracted()
	_ = ensureDriversScaffold(driversRoot())
}

// loadCatalog scans driversRoot for Windows drivers (.inf-shaped -
// driver.Catalog plus a Kyocera model index) and assigns the result under
// catalogMu. Called from startup() and RefreshDriverCatalog
// (drivercatalog_windows.go).
//
// Skips the ensure*Extracted archive-extraction helpers entirely
// (BuildCatalogNoExtract) when this exact running PDT.exe sits on a
// removable (USB flash) drive - confirmed live: on a USB 2.0 flash drive,
// the on-launch scan could take a long time and threw up a visible
// expand.exe console window per MSI, even when every archive on the drive
// was already extracted. A flash drive's Drivers folder is only ever
// populated by Write to Flash Drive/Sync from a technician's local install
// (postSyncDriversHook always extracts fully - see its own doc comment), and
// those flash drives carry a physical write-protect switch, so it's safe to
// assume everything on one is already extracted by the time PDT itself runs
// from it. A local/fixed-drive install still gets the full extracting scan.
//
// Also builds the macOS-shaped catalog (driver.MacCatalog/MacModelIndex),
// exactly the same two calls app_darwin.go's own loadCatalog makes, now
// possible on Windows too (GitHub issue #3): the four OS-tool seams
// indexFamilyPackage's own call chain depends on (openDmg, expandPkg,
// cpioExtractGlob/cpioExtractAll/cpioListEntries) all have real Windows-
// native implementations as of Phases 1-3. Catalog-file-only by design (Ken's
// own explicit scope decision) - a.macCatalog/a.macModelIndex are populated
// here exactly like app_darwin.go does, but nothing in drivercatalog_windows.go
// or the frontend ever reads them, so this has no UI effect at all: the real
// deliverable is catalog.<mfg>.json getting written under driversRoot/macOS,
// ready to travel to a real Mac via the existing Sync/Cloud Sync machinery.
//
// Runs in the background, after loadCatalog itself has already returned -
// confirmed live as a real startup-time regression the day this landed: a
// real Drivers/macOS tree of several manufacturers' own installer packages
// added ~10 seconds to every single Windows launch (visible as a burst of
// 7z.exe console windows during "Initializing..."), for a result nothing on
// Windows even reads yet. Since a.ready gates every other bound method and
// is only closed once loadCatalog returns (app.go's own startup), blocking
// on this here would hold the whole UI hostage for work with no Windows-side
// consumer. catalogMu still guards the write below, same as any other
// concurrent catalog mutation (RefreshDriverCatalog can already land at any
// moment) - just on its own goroutine's own schedule instead of inline.
func (a *App) loadCatalog(driversRoot string) error {
	buildFn := driver.BuildCatalog
	isRemovable := false
	if exe, err := os.Executable(); err == nil {
		isRemovable = flashdrive.IsRemovableDrive(exe)
	}
	if isRemovable {
		buildFn = driver.BuildCatalogNoExtract
	}
	catalog, err := buildFn(driversRoot)
	if err != nil {
		return err
	}

	a.catalogMu.Lock()
	a.catalog = catalog
	a.modelIndex = driver.BuildModelIndex(catalog)
	a.catalogMu.Unlock()

	go func() {
		macCatalog, macModelIndex, macChanges := a.loadMacCatalogBestEffort(driversRoot, !isRemovable)
		a.catalogMu.Lock()
		a.macCatalog = macCatalog
		a.macModelIndex = macModelIndex
		a.macModelChanges = macChanges
		a.catalogMu.Unlock()

		// Never run at all from removable media (GitHub issue #16 follow-up,
		// 2026-09-19 - Ken's own explicit ask) - see
		// buildOpenPrintingCatalogAsync's own doc comment for why this is a
		// full skip, not just persist=false. Chained after the mac catalog
		// itself is already assigned above (not run concurrently with it)
		// since it needs that catalog's own OpenPrintingPPDs to know what to
		// check - still entirely off a.ready's own critical path either way.
		if !isRemovable {
			a.buildOpenPrintingCatalogAsync(macCatalog, filepath.Join(driversRoot, "macOS"))
		}
	}()

	return nil
}

// loadMacCatalogBestEffort is loadCatalog's own macOS-side step, split out
// so a failure there (a missing/corrupt Drivers/macOS tree, a 7z.exe that
// hasn't been cached yet) degrades to an empty catalog rather than failing
// the Windows .inf catalog build it runs alongside - mirrors
// BuildMacCatalog's own "a missing driversRoot or macOS subfolder is an
// empty catalog, not an error" contract one level up, for the one real
// failure mode that's new here (driver.BuildMacCatalog itself already
// returns a real error only for a driversRoot os.ReadDir failure other than
// not-exist).
func (a *App) loadMacCatalogBestEffort(driversRoot string, persist bool) (driver.MacCatalog, driver.MacModelIndex, []string) {
	catalog, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return driver.MacCatalog{}, driver.MacModelIndex{}, nil
	}
	ppdCacheDir := ""
	if dir := installedAppDataDir(); dir != "" {
		ppdCacheDir = filepath.Join(dir, "PPDCache")
	}
	macRoot := filepath.Join(driversRoot, "macOS")
	modelIndex, changes := driver.BuildMacModelIndex(catalog, macRoot, ppdCacheDir, persist)
	return catalog, modelIndex, changes
}

// resolveOtherPlatformDriver is runbook.go's own resolveOtherPlatformDriverFunc
// seam on Windows: looks up manufacturer/model against the macOS-shaped
// catalog this same build already indexes in the background (see loadCatalog
// above) - the one platform that can answer "what would the *other*
// platform's driver be for this row" at all, since a macOS build never
// builds a Windows catalog. driverLabel "" (no pinned commitment) asks for
// whatever MacVariantForDeploy's own default preference order would pick -
// the Runbook is a point-in-time snapshot for a site survey, not a real
// deploy with a saved commitment to honor. ok is false whenever nothing
// resolves at all (not model-driven, no match, or the background catalog
// build just hasn't finished yet on a very fresh launch - see loadCatalog's
// own doc comment on why this runs asynchronously).
func (a *App) resolveOtherPlatformDriver(manufacturer, model string) (string, bool) {
	a.catalogMu.RLock()
	index := a.macModelIndex
	a.catalogMu.RUnlock()
	variant, ok := driver.MacVariantForDeploy(index, manufacturer, model, "")
	if !ok {
		return "", false
	}
	return variant.Label, true
}

// newPlatformDeployer builds this run's Deployer against the current Windows
// driver catalog - Deploy (app.go) calls this once per run rather than
// hard-coding pdtwin.NewDeployer directly, so app.go itself stays platform-
// independent.
func (a *App) newPlatformDeployer() printer.Deployer {
	catalog, _, _ := a.catalogSnapshot()
	return pdtwin.NewDeployer(catalog, configsRoot())
}

// postSyncDriversHook scaffolds the Windows-shaped Drivers\Windows\11\
// <Manufacturer> tree on driversDest (a flash drive's own freshly-copied
// Drivers folder) if needed, then runs driver.BuildCatalog against it purely
// for its side effects (zip/self-extracting-archive/msi/Kyocera auto-
// extraction - see scanManufacturerFolders) - the resulting catalog is
// discarded, this instance's own a.catalog is untouched, but any raw archive
// that just got copied onto the flash drive is extracted right there, so
// it's immediately usable without being plugged into another computer first
// just to trigger that. Called from flashdrive.go's syncDriversTo.
func postSyncDriversHook(driversDest string) error {
	if err := ensureDriversScaffold(driversDest); err != nil {
		return fmt.Errorf("scaffolding Drivers: %w", err)
	}
	_, _ = driver.BuildCatalog(driversDest)
	return nil
}
