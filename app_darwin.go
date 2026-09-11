package main

import (
	"path/filepath"

	"PDT/internal/driver"
	"PDT/internal/printer"
	pdtdarwin "PDT/internal/printer/darwin"
)

// platformStartup: nothing needed here yet - unlike Windows, there's no
// bundled extraction tool (.dmg/.pkg driver packages need no 7-Zip-style
// unpacking), no self-update mechanism to clean up after (see this port's
// own "explicitly out of scope" list - app self-update has no macOS analog
// yet), and no macOS-shaped Drivers-folder scaffold built yet either (a
// direct analog of driversfolder.go's ensureDriversScaffold, deferred until
// there's real demand for auto-creating Drivers/macOS/<Manufacturer>/... on
// first launch).
func (a *App) platformStartup() {}

// loadCatalog scans driversRoot for macOS driver packages (installer-
// package-shaped - driver.MacCatalog, see internal/driver/maccatalog.go),
// then builds the model index on top of it (driver.BuildMacModelIndex -
// Canon's own UFR II/PostScript/Generic PPD split today, see
// internal/driver/macmodel.go), and assigns both under catalogMu. Called
// from startup() and RefreshDriverCatalog (drivercatalog_darwin.go). The
// model index's own cache directory (for a no-installer family's PPDs - see
// MacPPDVariant's own doc comment) lives under installedAppDataDir() even
// for a portable/flash-drive copy, same reasoning driversRoot()/configsRoot()
// don't apply to it: it's a derived, rebuildable artifact, not something a
// technician's own flash-drive Drivers folder should carry around.
func (a *App) loadCatalog(driversRoot string) error {
	catalog, err := driver.BuildMacCatalog(driversRoot)
	if err != nil {
		return err
	}
	cacheDir := ""
	if dir := installedAppDataDir(); dir != "" {
		cacheDir = filepath.Join(dir, "PPDCache")
	}
	modelIndex := driver.BuildMacModelIndex(catalog, cacheDir)
	a.catalogMu.Lock()
	a.macCatalog = catalog
	a.macModelIndex = modelIndex
	a.catalogMu.Unlock()
	return nil
}

// newPlatformDeployer builds this run's Deployer against the current macOS
// driver catalog - Deploy (app.go) calls this once per run rather than
// hard-coding pdtdarwin.NewDeployer directly, so app.go itself stays
// platform-independent.
func (a *App) newPlatformDeployer() printer.Deployer {
	catalog, modelIndex, _ := a.macCatalogSnapshot()
	return pdtdarwin.NewDeployer(catalog, modelIndex)
}

// sevenZipToolsDir: no bundled 7-Zip on macOS at all (sevenzip_windows.go) -
// "" makes writePortablePDTTo's own dirExists(sevenZipToolsDir()) guard
// (flashdrive.go) naturally skip copying it onto a flash drive, with no
// platform branch needed in that shared function itself.
func sevenZipToolsDir() string { return "" }

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
