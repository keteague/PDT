package driver

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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

	// Kyocera ships exactly one real driver package (its own "Web Build"
	// download, e.g. "Kyocera Web build 2026.07.03.dmg") - no genuinely
	// distinct driver families to choose between the way Canon has. A
	// single "Kyocera" token still gets it a real model index (see
	// BuildMacModelIndex) rather than the guess-based post-install fallback
	// every other single-package manufacturer uses: classifyMacFamily's
	// substring match against "Kyocera" trivially matches any real Kyocera
	// download's own filename (the manufacturer's own name is always in
	// there), so newestInFamily degrades to "whichever package is newest"
	// the same way ResolveMac's plain single-package case already works -
	// this is purely what unlocks the model-index code path, not a real
	// multi-family preference list.
	"Kyocera": {"Kyocera"},

	// Ricoh ships many small, independent downloads side by side, each
	// covering its own disjoint set of models - not version variants of one
	// driver the way Canon's UFRII/PS/PPD are. See ricohFamilyTokens'
	// (macricoh.go) own doc comment for the full story and why each token
	// is a real, verified-collision-free filename fragment.
	"Ricoh": ricohFamilyTokens,

	// Sharp ships exactly one real driver package, but unlike Kyocera its
	// own filename doesn't contain the manufacturer's own name at all -
	// confirmed live (2026-09-13) against every real file across all 8
	// OS-version folders in the actual Drivers folder that only two distinct
	// filenames ever appear: "MX-C55c_2512a_MacPS.dmg" (the real driver -
	// its own jp.co.sharp.document.mx-c55_1015-.pkg sub-package holds 147
	// real Sharp PPDs, confirmed via *NickName, covering nearly Sharp's
	// whole current BP-/MX- lineup) and
	// "Generic_GUC_PrinterSoftware_11202025.dmg" (a Lexmark-licensed,
	// white-labeled generic print-dialog-enhancement package - its own
	// PackageInfo bundle list references com.lexmark.ColorSeriesProductConfig
	// - confirmed to hold zero real PPDs at all, and the exact file the
	// guess-based ResolveMac newest-by-mtime fallback was wrongly
	// auto-populating into the Driver field before this table existed).
	// "MacPS" is a real, collision-free substring of the real driver's own
	// filename that never appears in the decoy's - the same classifyMacFamily
	// mechanism every other manufacturer's tokens already use, just with a
	// token drawn from the driver's own filename shape instead of the
	// manufacturer's name. Every one of Sharp's own real PPDs' *NickName
	// carries a generic, non-language " PPD" suffix (e.g.
	// "SHARP MX-3071S PPD") that isn't a distinguishing family the way
	// Canon's PS/PPD/UFRII suffixes are - "PPD" is listed second purely so
	// stripLanguageSuffix strips it from the friendly model name (the same
	// suffix-stripping trick Canon's own real "PPD" family already relies
	// on), not because any real Sharp filename ever matches it as a
	// classification token (confirmed none do, so it's permanently an empty,
	// harmless second family).
	"Sharp": {"MacPS", "PPD"},
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

// familyCandidates narrows packages down to just the ones classifyMacFamily
// assigns to family, then to just the ones compatible with whichever
// machine this process is actually running on right now
// (filterToCurrentOSVersionFolder) - the shared first step both
// newestInFamily and packagesInFamily build on, so a package placed for a
// different macOS release never gets picked, whether by the guess-based
// fallback (ResolveMacFamily) or the catalog-driven multi-version index
// (BuildMacModelIndex). Confirmed live as a real bug (2026-09-13): a
// technician running PDT from a synced flash drive on a client endpoint
// still on an older macOS release than the laptop that built the catalog
// was seeing (and, before this existed, could have had installed) a driver
// placed specifically for a *different* OS release entirely, with nothing
// distinguishing it as incompatible.
func familyCandidates(packages []MacPackage, tokens []string, family string) []MacPackage {
	var out []MacPackage
	for _, p := range packages {
		if classifyMacFamily(tokens, p.Path) == family {
			out = append(out, p)
		}
	}
	return filterToCurrentOSVersionFolder(out)
}

// newestInFamily is ResolveMac's own newest-by-mtime pick, narrowed to just
// the current machine's own compatible packages within family
// (familyCandidates).
func newestInFamily(packages []MacPackage, tokens []string, family string) (MacPackage, bool) {
	var newest MacPackage
	found := false
	for _, p := range familyCandidates(packages, tokens, family) {
		if !found || p.ModTime.After(newest.ModTime) {
			newest, found = p, true
		}
	}
	return newest, found
}

// packagesInFamily is newestInFamily's own sibling, returning every
// *distinct* compatible package within family rather than just the newest -
// newest first by file modification time. Exists so more than one
// compatible version of the same family can sit in the Drivers folder at
// once and still each be individually indexed/selectable (see
// MacModelIndex's own doc comment).
//
// Confirmed live against a real Drivers folder (2026-09-13) that this must
// deduplicate by (basename, size), not just return every MacPackage match
// verbatim: the established Drivers/macOS/<Manufacturer>/<OS-version>/...
// convention has a technician copy the *exact same* downloaded file into
// several OS-version folders side by side (one real Canon UFR II download
// showed up identically in 6 different OS-version folders) - without this,
// those 6 byte-identical copies each surfaced as their own "distinct
// coexisting version" in the Driver dropdown, which is real, confirmed-live
// data corruption this feature must never produce. Two copies sharing a
// basename and byte size are treated as the same logical download (the
// same "never silently modified in place" assumption IsCurrent's own doc
// comment already relies on for exactly this reason) - only the newest-
// mtime copy among them is kept as that version's own representative.
func packagesInFamily(packages []MacPackage, tokens []string, family string) []MacPackage {
	byIdentity := map[string]MacPackage{}
	for _, p := range familyCandidates(packages, tokens, family) {
		key := filepath.Base(p.Path) + "|" + strconv.FormatInt(p.Size, 10)
		if existing, ok := byIdentity[key]; !ok || p.ModTime.After(existing.ModTime) {
			byIdentity[key] = p
		}
	}
	out := make([]MacPackage, 0, len(byIdentity))
	for _, p := range byIdentity {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
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
		nick, _, matched := PackageBestModelScore(pkgPath, manufacturer, model)
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
