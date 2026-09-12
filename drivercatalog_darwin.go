package main

import (
	"sort"

	"PDT/internal/driver"
)

// macCatalogSnapshot returns the current macCatalog/macModelIndex/catalogErr
// under catalogMu's read lock - the darwin analog of catalogSnapshot
// (drivercatalog_windows.go).
func (a *App) macCatalogSnapshot() (driver.MacCatalog, driver.MacModelIndex, error) {
	a.catalogMu.RLock()
	defer a.catalogMu.RUnlock()
	return a.macCatalog, a.macModelIndex, a.catalogErr
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
	catalog, _, _ := a.macCatalogSnapshot()
	return CatalogStatus{OK: true, HasDrivers: len(driver.MacManufacturersWithPackages(catalog)) > 0}
}

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	catalog, _, catalogErr := a.macCatalogSnapshot()
	hasDrivers := len(driver.MacManufacturersWithPackages(catalog)) > 0
	if catalogErr != nil {
		return CatalogStatus{OK: false, Error: catalogErr.Error(), HasDrivers: hasDrivers}
	}
	return CatalogStatus{OK: true, HasDrivers: hasDrivers}
}

// Models is the grid row's Model field combobox's data source on macOS - the
// darwin analog of Windows' own Models (drivercatalog_windows.go), sourced
// from driver.MacModelIndex instead of Kyocera's .inf-derived one. Empty for
// every manufacturer except one macFamilyPreference lists (Canon today) -
// same "no rule -> plain free-text input" degrade the frontend's own
// combobox already gives Windows' non-Kyocera manufacturers.
func (a *App) Models(manufacturer, filterText string) []string {
	<-a.ready
	_, modelIndex, _ := a.macCatalogSnapshot()
	return driver.MacModels(modelIndex, manufacturer, filterText)
}

// DriverCandidates is the darwin data source for the Driver combobox (same
// bound method name/signature as Windows' - drivercatalog_windows.go - so
// the frontend's combobox wiring needs no platform branch at all). When
// model resolves to a real driver.MacModelIndex entry (a manufacturer
// macFamilyPreference lists, with a Model the technician has actually typed
// or picked - Canon today), offers that model's own language-variant labels
// (driver.MacModelCandidates - "<model> (UFR II)"/"(PostScript)"/
// "(Generic PPD)"), the same Model-narrows-Driver two-step Kyocera gets on
// Windows. Otherwise, when manufacturer has a local installer package with
// no model-index entry to narrow by, there's only one real candidate - the
// package resolves automatically (see
// internal/printer/darwin/deploy_darwin.go's resolveDriver) - so this offers
// its own label as the sole entry, fuzzy-filtered like everything else and
// unaffected by model (nothing to narrow among one candidate); when there's
// no package at all, falls back to every OpenPrinting PPD label for
// manufacturer, narrowed/ranked by both model and filterText (see
// OpenPrintingCandidates).
func (a *App) DriverCandidates(manufacturer, model, filterText string) []string {
	<-a.ready
	catalog, modelIndex, _ := a.macCatalogSnapshot()
	if candidates := driver.MacModelCandidates(modelIndex, manufacturer, model, filterText); len(candidates) > 0 {
		return candidates
	}
	if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		if driver.FuzzyMatchScore(resolved.Label, filterText) < 0 {
			return nil
		}
		return []string{resolved.Label}
	}
	return driver.OpenPrintingCandidates(catalog, manufacturer, model, filterText)
}

// DefaultDriverFor is the Defaults panel's pre-selected Driver value for
// manufacturer - the resolved package's own label when one is present
// locally, "" otherwise (an OpenPrinting-only manufacturer has no single
// obvious default among its PPDs, same as Models/DefaultDriverFor's own
// "nothing to pre-fill" case on Windows when a manufacturer has no rule).
//
// "" too - deliberately, and not treated as a missing value (see
// tip('driver') in frontend/src/main.js) - for a manufacturer with real
// per-model data (MacModelManufacturers below, Canon today): the Defaults
// panel has no Model field to narrow with at all, so there's no way to pick
// a genuinely correct single default among that manufacturer's several
// real driver packages (UFR II/PostScript/Generic PPD). This used to fall
// through to ResolveMac's own newest-by-mtime guess instead, which surfaced
// as a raw, unparsed package label like "UFRII_v10.19.25_mac" - not a real
// driver name at all, and a real, previously-shipped bug (see CHANGELOG).
// Every other manufacturer (one real package, no ambiguity to hide behind
// blank) keeps the ResolveMac guess exactly as before.
func (a *App) DefaultDriverFor(manufacturer string) string {
	<-a.ready
	catalog, modelIndex, _ := a.macCatalogSnapshot()
	if len(modelIndex[manufacturer]) > 0 {
		return ""
	}
	if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		return resolved.Label
	}
	return ""
}

// MacModelManufacturers lists every manufacturer with real per-model PPD
// data in the current model index (Canon today) - the frontend's own signal
// for which manufacturers make the grid's Model field mandatory and drive
// an auto-selected Driver once Model resolves (see tip('model')), and make
// the Defaults panel's own Driver field optional (DefaultDriverFor above) -
// without exposing macFamilyPreference's own internal manufacturer set
// directly.
func (a *App) MacModelManufacturers() []string {
	<-a.ready
	_, modelIndex, _ := a.macCatalogSnapshot()
	out := make([]string, 0, len(modelIndex))
	for mfg := range modelIndex {
		out = append(out, mfg)
	}
	sort.Strings(out)
	return out
}
