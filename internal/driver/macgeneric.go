package driver

// Apple's own generic PostScript/PCL drivers, bundled with CUPS itself
// (confirmed live via `lpinfo -m`, 2026-09-13: `drv:///sample.drv/generic.ppd`
// "Generic PostScript Printer" and `drv:///sample.drv/generpcl.ppd`
// "Generic PCL Laser Printer" - a compiled-in driver definition CUPS
// generates a real PPD from on demand, not a flat file sitting at a fixed
// path). Unlike any vendor package, these are part of the OS itself, so
// they're the one genuinely OS-version-proof choice available - present and
// identically named on every real macOS release this project has ever
// touched, old or new. `man lpadmin`: "-m model-name ... Models can be
// found using the lpinfo(8) command" - this is CUPS's own first-party,
// documented mechanism, not a guess.
//
// Offered as Driver-dropdown candidates *only* when nothing else is
// available at all on darwin's own native DriverCandidates (Ken's own
// explicit scoping, 2026-09-13) - see that function's own doc comment
// (drivercatalog_darwin.go) for exactly where this sits in its fallback
// chain; the macOS Driver modal's own MacDriverCandidatesFor
// (macdrivercandidate.go, package main) instead always offers Generic
// PostScript alongside whatever else resolves (Ken's own later revision,
// 2026-09-19). Never silently auto-picked either way: unlike every other
// fallback in this codebase, a technician has to explicitly select one from
// the dropdown, since PostScript vs. PCL is a real choice (not every
// printer supports both) this codebase has no way to guess.
//
// Labels are prefixed "Apple " (Ken's own explicit ask, 2026-09-19) so a
// technician can tell at a glance these are macOS's own bundled drivers,
// not something belonging to whichever manufacturer the row is otherwise
// showing candidates for - GenericDriverModelByLabel/GenericDriverCandidates'
// own callers all compare against these same constants, never a bare string
// literal, so this is the one place that needs to change for the prefix to
// round-trip everywhere.
const (
	GenericPostScriptLabel = "Apple Generic PostScript Printer"
	GenericPCLLabel        = "Apple Generic PCL Laser Printer"
	GenericPostScriptModel = "drv:///sample.drv/generic.ppd"
	GenericPCLModel        = "drv:///sample.drv/generpcl.ppd"
)

// GenericDriverCandidates returns Apple's own generic driver labels, fuzzy-
// filtered against filterText the same way every other Driver-dropdown
// source in this codebase is - []string{}, not nil, when filterText
// matches neither (same JSON-bridge reasoning MacModels/MacModelCandidates
// already document: a nil slice marshals to JSON null, and the frontend's
// own combobox calls .map()/.forEach() on whatever this resolves to with no
// defensive fallback).
func GenericDriverCandidates(filterText string) []string {
	options := []string{GenericPostScriptLabel, GenericPCLLabel}
	if filterText == "" {
		return options
	}
	out := []string{}
	for _, o := range options {
		if FuzzyMatchScore(o, filterText) >= 0 {
			out = append(out, o)
		}
	}
	return out
}

// GenericDriverModelByLabel maps a technician's own explicit Driver-dropdown
// selection back to the real `-m` model string EnsureQueue needs - ok is
// false for anything else (a real vendor label, a blank Driver, a typo),
// the caller's cue to keep resolving through the normal package-based
// paths instead.
func GenericDriverModelByLabel(label string) (model string, ok bool) {
	switch label {
	case GenericPostScriptLabel:
		return GenericPostScriptModel, true
	case GenericPCLLabel:
		return GenericPCLModel, true
	default:
		return "", false
	}
}
