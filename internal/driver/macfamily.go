package driver

import (
	"fmt"
	"path/filepath"
	"strings"
)

// macFamilyPreference: manufacturer -> driver "family" tokens, in preference
// order, for a manufacturer that ships more than one genuinely different
// driver as separate packages - not just version variants of the same one,
// which ResolveMac's plain newest-by-mtime pick already handles correctly.
// Confirmed real against Canon's actual macOS downloads: UFRII_v*.zip,
// PS_v*.zip, and PPDv*.zip are three distinct drivers (UFR II, PostScript,
// and a plain-PPD-only package), each supporting a different, overlapping-
// but-not-identical set of models - Ken's own stated preference order
// (UFR II, then PS, then PPD) is what this table encodes. A manufacturer not
// listed here has no family concept at all.
var macFamilyPreference = map[string][]string{
	"Canon": {"UFRII", "PS", "PPD"},
}

// classifyMacFamily returns which of tokens appears in path's own basename
// (case-insensitive substring), "" if none match - confirmed against real
// Canon filenames ("UFRII_v10.19.25_mac.zip", "PS_v4.17.24_mac.zip",
// "PPDv5.50_mac.zip") that a plain substring check unambiguously classifies
// all three with no collision between tokens.
func classifyMacFamily(tokens []string, path string) string {
	base := strings.ToUpper(filepath.Base(path))
	for _, tok := range tokens {
		if strings.Contains(base, strings.ToUpper(tok)) {
			return tok
		}
	}
	return ""
}

// newestInFamily is ResolveMac's own newest-by-mtime pick, narrowed to just
// the packages classifyMacFamily assigns to family.
func newestInFamily(packages []MacPackage, tokens []string, family string) (MacPackage, bool) {
	var newest MacPackage
	found := false
	for _, p := range packages {
		if classifyMacFamily(tokens, p.Path) != family {
			continue
		}
		if !found || p.ModTime.After(newest.ModTime) {
			newest, found = p, true
		}
	}
	return newest, found
}

// ResolveMacFamily is ResolveMac, but family-and-model-aware for a
// manufacturer with more than one genuinely distinct driver to choose
// between (see macFamilyPreference) - deploy_darwin.go's own resolveDriver
// calls this instead of ResolveMac directly for every manufacturer, not just
// Canon, since it degrades to exactly ResolveMac's own behavior (silently,
// note == "") for any manufacturer with no family table, or when model is
// blank (nothing to check a family's own PPDs against).
//
// For a manufacturer WITH a family table: tries each family in its own
// declared preference order, pre-inspecting its newest package's own PPD
// payload for model (PackageBestModelScore - read-only, mounts/expands but
// never installs anything) - the first family with any plausible match wins.
// note is only non-empty when there's something worth telling a technician:
// falling back from the *preferred* family to a lower-preference one that
// actually matched, or - if no family's payload matches model at all -
// falling back to the newest-overall pick with no confidence it's even the
// right driver (deploy_darwin.go logs this as a [WARN], the same treatment
// choosePPD's own "ambiguous" result already gets).
func ResolveMacFamily(catalog MacCatalog, manufacturer, model string) (resolved *ResolvedMacPackage, note string) {
	tokens := macFamilyPreference[manufacturer]
	if len(tokens) == 0 {
		return ResolveMac(catalog, manufacturer), ""
	}
	if model == "" {
		return ResolveMac(catalog, manufacturer), fmt.Sprintf(
			"%s ships more than one driver family (%s) and Model is blank, so the preference order (%s) can't be checked against anything - picked whichever package is newest instead.",
			manufacturer, strings.Join(tokens, "/"), strings.Join(tokens, " > "))
	}

	packages := catalog.Packages[manufacturer]
	for _, family := range tokens {
		pkg, ok := newestInFamily(packages, tokens, family)
		if !ok {
			continue
		}
		pkgPath, cleanup, err := LocatePkg(pkg.Path)
		if err != nil {
			cleanup()
			continue
		}
		nick, _, matched := PackageBestModelScore(pkgPath, model)
		cleanup()
		if !matched {
			continue
		}
		note = ""
		if family != tokens[0] {
			note = fmt.Sprintf("preferred %s family has no PPD matching model %q; using %s instead (matched %q).", tokens[0], model, family, nick)
		}
		return &ResolvedMacPackage{Path: pkg.Path, Kind: pkg.Kind, Label: PackageLabel(pkg.Path)}, note
	}

	return ResolveMac(catalog, manufacturer), fmt.Sprintf(
		"none of %s's driver families (%s) declare a PPD matching model %q - picked whichever package is newest instead; deploy may install the wrong driver.",
		manufacturer, strings.Join(tokens, "/"), model)
}
