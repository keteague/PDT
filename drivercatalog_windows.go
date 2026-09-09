package main

import (
	"sort"

	"PDT/internal/driver"
)

// catalogSnapshot returns the current catalog/modelIndex/catalogErr under
// catalogMu's read lock - every method below that reads any of the three
// goes through this rather than touching the fields directly, so a
// RefreshDriverCatalog call landing concurrently can't be observed half
// swapped-in.
func (a *App) catalogSnapshot() (driver.Catalog, map[string]map[string][]string, error) {
	a.catalogMu.RLock()
	defer a.catalogMu.RUnlock()
	return a.catalog, a.modelIndex, a.catalogErr
}

// RefreshDriverCatalog re-scans the Drivers folder in place - the toolbar's
// Refresh button, for picking up a driver package dropped in (or downloaded
// via Check for Updates) without restarting PDT, which was previously the
// only way (BuildCatalog only ever ran once, at startup). This calls the
// exact same driver.BuildCatalog startup already does (via loadCatalog), so
// it also re-runs every auto-extraction step (zip/self-extracting-archive/
// msi/Kyocera) on whatever's newly sitting in the Drivers folder, not just
// re-scanning already-extracted .inf files - dropping in a raw, never-
// extracted .zip or driver .exe and clicking Refresh is enough, no manual
// extraction needed first. Returns the same CatalogStatus shape
// GetCatalogStatus does, so the frontend can drive its no-drivers banner and
// re-populate any open Driver dropdowns from one call.
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
	catalog, _, _ := a.catalogSnapshot()
	return CatalogStatus{OK: true, HasDrivers: len(driver.ManufacturersWithDrivers(catalog)) > 0}
}

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	catalog, _, catalogErr := a.catalogSnapshot()
	hasDrivers := len(driver.ManufacturersWithDrivers(catalog)) > 0
	if catalogErr != nil {
		return CatalogStatus{OK: false, Error: catalogErr.Error(), HasDrivers: hasDrivers}
	}
	return CatalogStatus{OK: true, HasDrivers: hasDrivers}
}

// Models lists the known models for manufacturer (Kyocera only - other
// manufacturers' driver names aren't model-specific; see driver.ModelFromDriverName).
func (a *App) Models(manufacturer string) []string {
	<-a.ready
	_, modelIndex, _ := a.catalogSnapshot()
	byModel, ok := modelIndex[manufacturer]
	if !ok {
		return nil
	}
	models := make([]string, 0, len(byModel))
	for m := range byModel {
		models = append(models, m)
	}
	sort.Strings(models)
	return models
}

// DriverCandidates lists selectable driver labels for a row's dropdown -
// plain names, or decorated "<name> (vVersion - date)" labels when more than
// one arch-compatible local version exists. filterText fuzzy-matches and
// re-ranks when non-empty (free-text typing in the dropdown).
func (a *App) DriverCandidates(manufacturer, model, filterText string) []string {
	<-a.ready
	catalog, modelIndex, _ := a.catalogSnapshot()
	return driver.Candidates(catalog, modelIndex, manufacturer, model, filterText)
}

// DefaultDriverFor is the Defaults panel's pre-selected driver name for
// manufacturer (e.g. Canon -> its UFR II driver), or "" if there's no such
// rule for manufacturer or no matching driver is present locally.
func (a *App) DefaultDriverFor(manufacturer string) string {
	<-a.ready
	catalog, _, _ := a.catalogSnapshot()
	return driver.DefaultDriverNameFor(catalog, manufacturer)
}
