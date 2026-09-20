package main

import (
	"path/filepath"
	"strings"

	"PDT/internal/driver"
)

// MacDriverCandidate is one selectable entry in the macOS Driver modal's own
// dropdown (GitHub issue #16 follow-up - Ken's own ask, 2026-09-19). Source
// is a human-readable path to the real package/PPD this candidate actually
// comes from, relative to Drivers/macOS (e.g.
// "Canon\26-Tahoe\UFRII_v10.19.25_mac.zip" or
// "OpenPrinting\Canon\cnadvc5250x1g.ppd") - or a plain descriptive string
// for Apple's own bundled Generic PostScript/PCL fallback, which has no real
// package file behind it at all (macDriverCandidateGenericSource). Shown as
// that dropdown item's own tooltip (main.js) so a technician overriding the
// modal's auto-picked default can see exactly what they're choosing between,
// not just a bare label.
type MacDriverCandidate struct {
	Label  string `json:"label"`
	Source string `json:"source"`
}

// macDriverCandidateGenericSource is MacDriverCandidate.Source's value for
// one of GenericDriverCandidates' own entries (macgeneric.go) - these are
// bundled with macOS itself, never a real file under the Drivers folder.
const macDriverCandidateGenericSource = "Apple (built-in Generic driver)"

// macDriverCandidateSource turns an absolute package/PPD path into
// MacDriverCandidate.Source's own display form - relative to
// driversRoot()/macOS, the real folder a technician already sees on disk
// (matching the Drivers/macOS/<Manufacturer>/<OS-version>/... and
// Drivers/macOS/OpenPrinting/<Manufacturer>/... conventions this codebase
// already commits to elsewhere). Falls back to the bare filename if absPath
// somehow isn't under there (defensive - a tooltip showing just a filename
// is still more useful than one showing nothing).
func macDriverCandidateSource(absPath string) string {
	macRoot := filepath.Join(driversRoot(), "macOS")
	if rel, err := filepath.Rel(macRoot, absPath); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return filepath.Base(absPath)
}

// openPrintingCandidateWithSource builds one OpenPrinting entry's own
// MacDriverCandidate - GitHub issue #16 follow-up (Ken's own ask,
// 2026-09-19): the dropdown should show the PPD's own real cached
// *NickName/*ModelName content (d.Label is otherwise ppdMatchLabel's
// filename-derived form - see OpenPrintingCandidateDetail's own doc
// comment) as its primary label whenever one is cached, since that's the
// human-recognizable model name a technician actually expects to read - the
// filename-derived label moves into the tooltip instead (alongside the
// existing source path, which Ken separately asked for), rather than
// disappearing outright. A PPD BuildOpenPrintingNickNames hasn't cataloged
// yet, or one with no *NickName/*ModelName field at all, falls back to
// exactly the pre-existing behavior: the filename-derived label shown
// as-is, tooltip carrying just the path.
func openPrintingCandidateWithSource(catalog driver.MacCatalog, manufacturer string, d driver.OpenPrintingCandidateDetail) MacDriverCandidate {
	path := macDriverCandidateSource(d.Path)
	if nick := catalog.OpenPrintingNickNames[manufacturer][d.Path]; nick != "" {
		return MacDriverCandidate{Label: nick, Source: d.Label + "\n" + path}
	}
	return MacDriverCandidate{Label: d.Label, Source: path}
}

// macDriverCandidatesWithSource is the macOS Driver modal's own shared
// candidate-gathering logic (GitHub issue #16 follow-up, 2026-09-19) - one
// implementation for both platforms' own MacDriverCandidatesFor bound method
// (drivercatalog_windows.go/drivercatalog_darwin.go), which differ only in
// how they snapshot catalog/modelIndex. Mirrors DriverCandidates' own
// existing chain (driver.MacModelCandidateDetails, else driver.ResolveMac;
// driver.OpenPrintingCandidateDetails appended next) but carries each
// candidate's own real source path/package through for MacDriverCandidate.Source,
// which DriverCandidates' own plain-[]string return can't - and always
// offers Apple's own bundled Generic PostScript driver too (see the doc
// comment right below), not just as a last resort.
func macDriverCandidatesWithSource(catalog driver.MacCatalog, modelIndex driver.MacModelIndex, manufacturer, model, filterText string) []MacDriverCandidate {
	var out []MacDriverCandidate
	if variants := driver.MacModelCandidateDetails(modelIndex, manufacturer, model, filterText); len(variants) > 0 {
		for _, v := range variants {
			out = append(out, MacDriverCandidate{Label: v.Label, Source: macDriverCandidateSource(v.SourcePackagePath)})
		}
	} else if resolved := driver.ResolveMac(catalog, manufacturer); resolved != nil {
		if driver.FuzzyMatchScore(resolved.Label, filterText) >= 0 {
			out = append(out, MacDriverCandidate{Label: resolved.Label, Source: macDriverCandidateSource(resolved.Path)})
		}
	}
	for _, d := range driver.OpenPrintingCandidateDetails(catalog, manufacturer, model, filterText) {
		out = append(out, openPrintingCandidateWithSource(catalog, manufacturer, d))
	}

	// Apple's own bundled Generic PostScript driver is always offered here,
	// alongside whatever else resolved - GitHub issue #16 follow-up (Ken's
	// own explicit revision, 2026-09-19, of macgeneric.go's earlier
	// "only when nothing else resolves at all" scoping): the one genuinely
	// OS-version-proof choice available (see GenericDriverCandidates' own
	// doc comment), worth keeping visible even when a real vendor/
	// OpenPrinting candidate already exists - not appended a second time if
	// a vendor/OpenPrinting label already happens to collide with it
	// exactly (never realistically happens, but a plain duplicate entry in
	// the dropdown would be a worse outcome than skipping it).
	if driver.FuzzyMatchScore(driver.GenericPostScriptLabel, filterText) >= 0 {
		alreadyPresent := false
		for _, c := range out {
			if c.Label == driver.GenericPostScriptLabel {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			out = append(out, MacDriverCandidate{Label: driver.GenericPostScriptLabel, Source: macDriverCandidateGenericSource})
		}
	}

	if len(out) > 0 {
		return out
	}
	// Nothing at all resolved, not even Generic PostScript against
	// filterText - fall back to the complete generic set (PostScript+PCL),
	// exactly as before, so a technician typing something Generic PostScript
	// itself doesn't match still sees Generic PCL as an option rather than
	// a silently empty dropdown.
	for _, label := range driver.GenericDriverCandidates(filterText) {
		out = append(out, MacDriverCandidate{Label: label, Source: macDriverCandidateGenericSource})
	}
	return out
}
