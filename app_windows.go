package main

import (
	"fmt"
	"os"

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
func (a *App) loadCatalog(driversRoot string) error {
	buildFn := driver.BuildCatalog
	if exe, err := os.Executable(); err == nil && flashdrive.IsRemovableDrive(exe) {
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
	return nil
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
