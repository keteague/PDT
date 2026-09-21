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
// via Download Center) without restarting PDT, which was previously the
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

// ListRescanTargets: see driver.RescanManufacturer - the Rescan dialog's own
// manufacturer/package tree data source (replaces the old one-click Refresh
// Drivers button on Windows - see main.js's own btnRefreshDrivers handler).
func (a *App) ListRescanTargets() []driver.RescanManufacturer {
	<-a.ready
	return driver.ListRescanTargets(driversRoot())
}

// RescanDrivers applies the Rescan dialog's own selection: if removeInf is
// checked, best-effort clears the .pdt-infcache entries named in selected
// (see driver.RemoveInfCacheForSelection) so they're re-extracted fresh
// below; if removeCatalog is checked, best-effort deletes catalog.<mfg>.json
// for whichever manufacturers were selected (see
// driver.RemoveMacCatalogFilesForSelection), so the next mac-catalog build -
// on Windows, the background one RefreshDriverCatalog's own loadCatalog call
// kicks off (GitHub issue #3) - rebuilds that manufacturer's own catalog
// from scratch instead of reusing whatever was already there. Then always
// runs the exact same full rebuild RefreshDriverCatalog already does. A full
// rebuild rather than one scoped just to the selection: now that .inf-only
// extraction (GitHub issue #10) replaced full-package extraction, a full
// rescan is cheap regardless - bounded by archive count, not extracted-file
// count - so there's no performance reason for separate partial-rebuild
// machinery just to mirror the dialog's own selective removal scope.
//
// removeCatalog's own effect on Windows is backgrounded and silent, same as
// the mac-catalog build it forces fresh already always was here - the
// CatalogStatus this returns reflects the Windows .inf catalog only (see
// RefreshDriverCatalog's own doc comment); there's no separate completion
// signal for the mac-catalog side today, on either platform.
func (a *App) RescanDrivers(selected []string, removeInf, removeCatalog bool) CatalogStatus {
	<-a.ready
	if removeInf {
		driver.RemoveInfCacheForSelection(driversRoot(), selected)
	}
	if removeCatalog {
		driver.RemoveMacCatalogFilesForSelection(driversRoot(), selected)
	}
	return a.RefreshDriverCatalog()
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

// Models is the grid row's Model field combobox's data source - see
// driver.Models for the actual ranking logic and why an empty result is the
// expected, deliberate outcome for every manufacturer except Kyocera today.
func (a *App) Models(manufacturer, filterText string) []string {
	<-a.ready
	_, modelIndex, _ := a.catalogSnapshot()
	return driver.Models(modelIndex, manufacturer, filterText)
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

// DriverCandidatesWithSource is DriverCandidates' own sibling for the Driver
// modal's Windows Driver field - identical candidates and ordering, but each
// carries a tooltip naming its manufacturer and the real package it comes
// from, so several similarly-named drivers (e.g. three KONICA MINOLTA
// Universal PCL versions) can be told apart.
func (a *App) DriverCandidatesWithSource(manufacturer, model, filterText string) []WindowsDriverCandidate {
	<-a.ready
	catalog, modelIndex, _ := a.catalogSnapshot()
	details := driver.CandidateDetails(catalog, modelIndex, manufacturer, model, filterText)
	out := make([]WindowsDriverCandidate, len(details))
	for i, d := range details {
		out[i] = WindowsDriverCandidate{Label: d.Label, Source: windowsDriverCandidateSource(manufacturer, d.Sources)}
	}
	return out
}

// DefaultDriverFor is the Defaults panel's pre-selected driver name for
// manufacturer (e.g. Canon -> its UFR II driver), or "" if there's no such
// rule for manufacturer or no matching driver is present locally.
func (a *App) DefaultDriverFor(manufacturer string) string {
	<-a.ready
	catalog, _, _ := a.catalogSnapshot()
	return driver.DefaultDriverNameFor(catalog, manufacturer)
}

// macCatalogSnapshot returns the current macCatalog/macModelIndex under
// catalogMu's read lock - the Windows analog of drivercatalog_darwin.go's
// own macCatalogSnapshot, reading the same fields loadCatalog's own
// background goroutine populates (app_windows.go - GitHub issue #3). Unlike
// that goroutine's own populate-in-background timing, every method below
// blocks on <-a.ready first, same as every other bound method - a.ready only
// covers the Windows .inf catalog finishing, so a call landing in the first
// few seconds after startup can still see an empty macModelIndex if the
// background build hasn't caught up yet; every caller here already treats
// "nothing resolved" as a normal, valid outcome (a free-text field, not an
// error), so this is a graceful, self-correcting degrade, not a bug.
func (a *App) macCatalogSnapshot() (driver.MacCatalog, driver.MacModelIndex) {
	a.catalogMu.RLock()
	defer a.catalogMu.RUnlock()
	return a.macCatalog, a.macModelIndex
}

// MacModelManufacturers lists every manufacturer with real per-model macOS
// PPD data in the background-built mac catalog (GitHub issue #3) - identical
// role and implementation to drivercatalog_darwin.go's own method of the
// same name, now meaningful on Windows too (GitHub issue #16): the
// frontend's own macModelDriven() check no longer needs an isMac() gate to
// mean something here, since a Windows-authored row can now also pin a real
// macOS driver commitment for the same printer.
func (a *App) MacModelManufacturers() []string {
	<-a.ready
	_, modelIndex := a.macCatalogSnapshot()
	out := make([]string, 0, len(modelIndex))
	for mfg := range modelIndex {
		out = append(out, mfg)
	}
	sort.Strings(out)
	return out
}

// MacModelsFor is the new "macOS Driver" modal's own Model field data
// source when the row's manufacturer has real per-model data (see
// MacModelManufacturers) - identical to drivercatalog_darwin.go's own Models,
// just reading the Windows-side background-built mac catalog instead of a
// native mac build's own foreground one. Named distinctly from Windows' own
// Models (Kyocera's .inf-derived model list) since a Windows build needs
// both at once now - one row can resolve a Kyocera Windows model *and* a
// Canon-shaped macOS model for two entirely different purposes.
func (a *App) MacModelsFor(manufacturer, filterText string) []string {
	<-a.ready
	_, modelIndex := a.macCatalogSnapshot()
	return driver.MacModels(modelIndex, manufacturer, filterText)
}

// MacDriverCandidatesFor is the new "macOS Driver" modal's own field data
// source, once a technician configuring from Windows has picked (or typed) a
// Model - identical logic to drivercatalog_darwin.go's own MacDriverCandidatesFor
// (same OpenPrinting-always-appended, Generic-PostScript-last-resort
// fallback chain, and same bound method name/signature, so the frontend
// needs no platform branch to fetch these), reading the Windows-side
// background-built mac catalog. A blank model behaves exactly as
// MacModelCandidates' own doc comment describes (every variant of every
// model, preference-ordered - UFR II before PostScript before Generic PPD
// for Canon). Ordering additionally favors whichever real macOS release is
// actually newest (osVersionFolderRank, GitHub issue #16 follow-up,
// 2026-09-19 - "v27 Golden Gate over v26 Tahoe today") whenever more than
// one OS-version folder's packages coexist here, which they always do on
// Windows (see filterToCurrentOSVersionFolder's own doc comment) - falling
// back automatically to the next-newest release with an actual driver for
// this family/model. Each candidate also carries its own real source
// package/PPD path (MacDriverCandidate.Source) for the frontend's own
// tooltip - see macDriverCandidatesWithSource (macdrivercandidate.go).
func (a *App) MacDriverCandidatesFor(manufacturer, model, filterText string) []MacDriverCandidate {
	<-a.ready
	catalog, modelIndex := a.macCatalogSnapshot()
	return macDriverCandidatesWithSource(catalog, modelIndex, manufacturer, model, filterText)
}

// ModelCandidatesWithSource is the Driver modal's Model dropdown data source
// on Windows: Kyocera's .inf-derived models, the background-built macOS
// catalog's models, and OpenPrinting-derived ones, each with a tooltip naming
// the driver package(s) it comes from - see modelCandidatesWithSource.
func (a *App) ModelCandidatesWithSource(manufacturer, filterText string) []ModelCandidate {
	<-a.ready
	winCatalog, winModelIndex, _ := a.catalogSnapshot()
	macCatalog, macIndex := a.macCatalogSnapshot()
	return modelCandidatesWithSource(macCatalog, macIndex, winCatalog, winModelIndex, manufacturer, filterText)
}

// DriverProblems checks one grid row's Windows/macOS driver commitments
// against what the catalogs actually offer - the grid Driver button turns red
// (with the returned sentence as its tooltip) when either side reports a
// problem. Only platforms that are switched on for the row are checked.
func (a *App) DriverProblems(manufacturer, model, winDriver, macDriver string, windowsEnabled, macEnabled bool) RowDriverProblems {
	<-a.ready
	var out RowDriverProblems
	if windowsEnabled {
		catalog, modelIndex, _ := a.catalogSnapshot()
		out.Windows = windowsDriverProblem(catalog, modelIndex, manufacturer, winDriver)
	}
	if macEnabled {
		macCatalog, macIndex := a.macCatalogSnapshot()
		out.Mac = macDriverProblem(macCatalog, macIndex, manufacturer, model, macDriver)
	}
	return out
}
