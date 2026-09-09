package main

import "PDT/internal/driver"

// macCatalogSnapshot returns the current macCatalog/catalogErr under
// catalogMu's read lock - the darwin analog of catalogSnapshot
// (drivercatalog_windows.go).
func (a *App) macCatalogSnapshot() (driver.MacCatalog, error) {
	a.catalogMu.RLock()
	defer a.catalogMu.RUnlock()
	return a.macCatalog, a.catalogErr
}

// RefreshDriverCatalog re-scans the Drivers folder in place - the toolbar's
// Refresh button, same role as the Windows one (drivercatalog_windows.go)
// but over the macOS-shaped catalog.
func (a *App) RefreshDriverCatalog() CatalogStatus {
	<-a.ready
	if err := a.loadCatalog(driversRoot()); err != nil {
		a.catalogMu.Lock()
		a.catalogErr = err
		a.catalogMu.Unlock()
		return CatalogStatus{OK: false, Error: err.Error()}
	}
	a.catalogMu.Lock()
	a.catalogErr = nil
	a.catalogMu.Unlock()
	catalog, _ := a.macCatalogSnapshot()
	return CatalogStatus{OK: true, HasDrivers: len(driver.MacManufacturersWithPackages(catalog)) > 0}
}

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	catalog, catalogErr := a.macCatalogSnapshot()
	hasDrivers := len(driver.MacManufacturersWithPackages(catalog)) > 0
	if catalogErr != nil {
		return CatalogStatus{OK: false, Error: catalogErr.Error(), HasDrivers: hasDrivers}
	}
	return CatalogStatus{OK: true, HasDrivers: hasDrivers}
}

// DriverCandidates is the darwin data source for the Driver combobox (same
// bound method name/signature as Windows' - drivercatalog_windows.go - so
// the frontend's combobox wiring needs no platform branch at all). When
// manufacturer has a local installer package, there's only one real
// candidate - the package resolves automatically (see
// internal/printer/darwin/deploy_darwin.go's resolveDriver) - so this offers
// its own label as the sole entry, fuzzy-filtered like everything else;
// when there's no package, falls back to every OpenPrinting PPD label for
// manufacturer, ranked against filterText.
func (a *App) DriverCandidates(manufacturer, model, filterText string) []string {
	<-a.ready
	catalog, _ := a.macCatalogSnapshot()
	if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		if driver.FuzzyMatchScore(resolved.Label, filterText) < 0 {
			return nil
		}
		return []string{resolved.Label}
	}
	return driver.OpenPrintingCandidates(catalog, manufacturer, filterText)
}

// DefaultDriverFor is the Defaults panel's pre-selected Driver value for
// manufacturer - the resolved package's own label when one is present
// locally, "" otherwise (an OpenPrinting-only manufacturer has no single
// obvious default among its PPDs, same as Models/DefaultDriverFor's own
// "nothing to pre-fill" case on Windows when a manufacturer has no rule).
func (a *App) DefaultDriverFor(manufacturer string) string {
	<-a.ready
	catalog, _ := a.macCatalogSnapshot()
	if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		return resolved.Label
	}
	return ""
}
