package main

import (
	"os"
	"path/filepath"

	"PDT/internal/driver"
	"PDT/internal/flashdrive"
	"PDT/internal/printer"
	pdtdarwin "PDT/internal/printer/darwin"
)

// platformStartup: unlike Windows, there's no bundled extraction tool
// (.dmg/.pkg driver packages need no 7-Zip-style unpacking) and no
// self-update mechanism to clean up after (see this port's own "explicitly
// out of scope" list - app self-update has no macOS analog yet). It does
// now scaffold the macOS-shaped Drivers tree (driversfolder.go's
// ensureMacDriversScaffold - a direct, if differently-shaped, analog of the
// Windows side's own ensureDriversScaffold), best-effort: a scaffold failure
// (e.g. a read-only Drivers folder) shouldn't block startup any more than
// the Windows side's own best-effort ensureDriversScaffold call does.
//
// Also sweeps for and detaches any stale mount an earlier, improperly-
// terminated run left behind (GitHub issue #1, driver.ReconcileStaleMounts's
// own doc comment for the full story) - before loadCatalog gets a chance to
// mount anything new, both because a stray CANON_MAC left over from a
// previous run would otherwise force a fresh mount of the same real volume
// to rename itself CANON_MAC 1, and because there's no reason to leave a
// leaked read-only mount sitting around a moment longer than it has to once
// this run has the chance to clean it up.
func (a *App) platformStartup() {
	_ = ensureMacDriversScaffold(filepath.Join(driversRoot(), "macOS"))
	driver.ReconcileStaleMounts(filepath.Join(driversRoot(), "macOS"))
}

// loadCatalog scans driversRoot for macOS driver packages (installer-
// package-shaped - driver.MacCatalog, see internal/driver/maccatalog.go),
// then builds the model index on top of it (driver.BuildMacModelIndex -
// Canon's own UFR II/PostScript/Generic PPD split today, see
// internal/driver/macmodel.go), and assigns both (plus any model-change
// summary) under catalogMu. Called from startup() and RefreshDriverCatalog
// (drivercatalog_darwin.go).
//
// BuildMacModelIndex gets two different directories for two different
// reasons:
//   - macRoot (driversRoot/macOS) is where each manufacturer's own
//     catalog.json lives (CatalogFileName) - inside the Drivers folder
//     itself, deliberately, so it travels with a portable/flash-drive copy
//     (see MacManufacturerCatalog's own doc comment).
//   - ppdCacheDir (a no-installer family's cached PPD bytes - see
//     MacPPDVariant's own doc comment) stays under installedAppDataDir()
//     even for a portable copy, same reasoning driversRoot()/configsRoot()
//     don't apply to it: a flash drive is normally write-protected in the
//     field (driversfolder.go's own ensureMacDriversScaffold doc comment),
//     and it's cheap enough to rebuild locally now (packagePPDEntries' own
//     doc comment) that there's no real benefit to it traveling too -
//     cachedVariantFilesExist already covers the "someone else's
//     catalog.json references a file this machine doesn't have yet" case.
//
// persist (whether a changed catalog actually gets written back to
// macRoot) is false whenever this exact running copy is on a removable
// drive - a technician's own laptop is where catalog.json gets built/
// updated; a flash drive plugged into a different machine only ever reads
// whatever's already there, the same "USB is slow/write-protected, don't
// redo or rewrite expensive work every launch" reasoning
// BuildCatalogNoExtract already applies on the Windows side
// (app_windows.go's own loadCatalog).
func (a *App) loadCatalog(driversRoot string) error {
	catalog, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return err
	}
	ppdCacheDir := ""
	if dir := installedAppDataDir(); dir != "" {
		ppdCacheDir = filepath.Join(dir, "PPDCache")
	}
	macRoot := filepath.Join(driversRoot, "macOS")
	persist := true
	if exe, err := os.Executable(); err == nil {
		persist = !flashdrive.IsRemovableDrive(exe)
	}
	modelIndex, changes := driver.BuildMacModelIndex(catalog, macRoot, ppdCacheDir, persist)
	a.catalogMu.Lock()
	a.macCatalog = catalog
	a.macModelIndex = modelIndex
	a.macModelChanges = changes
	a.catalogMu.Unlock()

	// Backgrounded, not run inline here - unlike the vendor package model
	// index just above (needed immediately for the UI to function),
	// nothing blocks on the OpenPrinting nickname cache being ready, and
	// loadCatalog itself runs synchronously on this platform (blocks
	// a.ready - see startup(), app.go), unlike Windows' own already-
	// backgrounded mac-catalog goroutine. Never run at all from removable
	// media (persist false here means exactly that) - see
	// buildOpenPrintingCatalogAsync's own doc comment.
	if persist {
		go a.buildOpenPrintingCatalogAsync(catalog, macRoot)
	}
	return nil
}

// resolveOtherPlatformDriver is runbook.go's own resolveOtherPlatformDriverFunc
// seam on macOS: always "not found" - unlike Windows (which builds a
// macOS-shaped catalog alongside its own, see GitHub issue #3), a macOS
// build has no Windows driver catalog at all to resolve against, so the
// Runbook's own "Windows:" lines simply stay blank here, for the technician
// to fill in by hand.
func (a *App) resolveOtherPlatformDriver(manufacturer, model string) (string, bool) {
	return "", false
}

// newPlatformDeployer builds this run's Deployer against the current macOS
// driver catalog - Deploy (app.go) calls this once per run rather than
// hard-coding pdtdarwin.NewDeployer directly, so app.go itself stays
// platform-independent.
func (a *App) newPlatformDeployer() printer.Deployer {
	catalog, modelIndex, _ := a.macCatalogSnapshot()
	return pdtdarwin.NewDeployer(catalog, modelIndex)
}

// postSyncDriversHook: nothing needed here yet, matching platformStartup's
// own reasoning - there's no macOS Drivers scaffold built yet, and unlike
// Windows' .inf packages, a .dmg/.pkg driver package needs no pre-extraction
// step at catalog-scan time at all (internal/driver/maccatalog.go just
// records the file paths as-is; the actual .pkg gets located/mounted lazily,
// only when a Deploy actually installs it - see driver.LocatePkg). Called
// from flashdrive.go's syncDriversTo.
func postSyncDriversHook(driversDest string) error {
	return nil
}
