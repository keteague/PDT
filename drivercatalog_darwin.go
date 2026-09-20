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
	changes := a.macModelChanges
	a.catalogMu.Unlock()
	catalog, _, _ := a.macCatalogSnapshot()
	return CatalogStatus{OK: true, HasDrivers: len(driver.MacManufacturersWithPackages(catalog)) > 0, ModelChanges: changes}
}

func (a *App) GetCatalogStatus() CatalogStatus {
	<-a.ready
	catalog, _, catalogErr := a.macCatalogSnapshot()
	hasDrivers := len(driver.MacManufacturersWithPackages(catalog)) > 0
	if catalogErr != nil {
		return CatalogStatus{OK: false, Error: catalogErr.Error(), HasDrivers: hasDrivers}
	}
	a.catalogMu.RLock()
	changes := a.macModelChanges
	a.catalogMu.RUnlock()
	return CatalogStatus{OK: true, HasDrivers: hasDrivers, ModelChanges: changes}
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
// or picked), offers that model's own language-variant labels
// (driver.MacModelCandidates - "<model> (UFR II)"/"(PostScript)"/
// "(Generic PPD)"), the same Model-narrows-Driver two-step Kyocera gets on
// Windows. Otherwise, when manufacturer has a local installer package with
// no model-index entry to narrow by, there's only one real candidate - the
// package resolves automatically (see
// internal/printer/darwin/deploy_darwin.go's resolveDriver) - so this offers
// its own label as the sole entry, fuzzy-filtered like everything else and
// unaffected by model (nothing to narrow among one candidate).
//
// Every OpenPrinting PPD label for manufacturer that matches model/filterText
// (driver.OpenPrintingCandidates) is ALWAYS appended too, not just when
// nothing else resolved - Ken's own explicit ask (2026-09-16): a technician
// needs a way to override the auto-resolved real driver when it genuinely
// doesn't cover their printer's real model, rather than being stuck with
// only the one auto-picked option. Each carries its own trailing " (OP)"
// marker (ppdMatchLabel, macresolve.go) precisely so it's never mistaken for
// a real vendor driver once shown alongside one.
//
// Only when *nothing* resolves at all - no catalog/package match and no
// OpenPrinting PPD either - confirmed live as a real scenario, not
// hypothetical: a real vendor package existed but got correctly excluded by
// filterToCurrentOSVersionFolder (a driver built for a different macOS
// release than this machine is actually running, see MacPackage's own doc
// comment) - this falls all the way back to Apple's own bundled Generic
// PostScript/PCL drivers (macgeneric.go), the one genuinely OS-version-proof
// choice. Ken's own explicit scoping (2026-09-13, still true): these never
// show up alongside a real candidate and are never auto-picked (see
// resolveDriver's own doc comment) - a technician has to explicitly choose
// one, since PostScript vs. PCL is a real choice this codebase has no way to
// guess.
func (a *App) DriverCandidates(manufacturer, model, filterText string) []string {
	<-a.ready
	catalog, modelIndex, _ := a.macCatalogSnapshot()

	var out []string
	if candidates := driver.MacModelCandidates(modelIndex, manufacturer, model, filterText); len(candidates) > 0 {
		out = append(out, candidates...)
	} else if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		if driver.FuzzyMatchScore(resolved.Label, filterText) >= 0 {
			out = append(out, resolved.Label)
		}
	}
	out = append(out, driver.OpenPrintingCandidates(catalog, manufacturer, model, filterText)...)

	if len(out) > 0 {
		return out
	}
	return driver.GenericDriverCandidates(filterText)
}

// MacDriverCandidatesFor is the Driver modal's own dedicated "macOS Driver"
// field data source (GitHub issue #16 follow-up, 2026-09-19) - identical
// bound method name/signature to Windows' own (drivercatalog_windows.go), so
// the frontend needs no platform branch to fetch macOS Driver candidates
// either way. Distinct from DriverCandidates above (which the modal's own
// Windows Driver field keeps using unchanged, on both platforms): that
// returns plain labels with no source-path metadata, while this additionally
// carries each candidate's own real package/PPD path
// (MacDriverCandidate.Source) for the frontend's own tooltip - see
// macDriverCandidatesWithSource (macdrivercandidate.go), the exact same
// gathering logic Windows' own MacDriverCandidatesFor calls.
func (a *App) MacDriverCandidatesFor(manufacturer, model, filterText string) []MacDriverCandidate {
	<-a.ready
	catalog, modelIndex, _ := a.macCatalogSnapshot()
	return macDriverCandidatesWithSource(catalog, modelIndex, manufacturer, model, filterText)
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
