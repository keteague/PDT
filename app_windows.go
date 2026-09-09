package main

import (
	"fmt"
	"os"

	"PDT/internal/driver"
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
func (a *App) loadCatalog(driversRoot string) error {
	catalog, err := driver.BuildCatalog(driversRoot)
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
