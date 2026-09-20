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

	// Xerox ships exactly one real driver package, periodically superseded -
	// confirmed live (2026-09-13) against real files across 8 real OS-version
	// folders: "XeroxDrivers_5.6.0_2187.dmg" through "..._5.19.3_2562.dmg",
	// same "one driver line" shape as Kyocera, not Canon's genuinely distinct
	// UFRII/PS/PPD split or Ricoh's many-small-disjoint-downloads one. Like
	// Kyocera, the manufacturer's own name is always in the real filename, so
	// a single "Xerox" token trivially classifies it - this unlocks the
	// catalog-driven model index (BuildMacModelIndex), not a real
	// multi-family preference list. The real package's own single sub-package
	// (identifier "com.xerox.drivers.pkg", install-location "/") holds 178
	// real Xerox PPDs alongside ~6,371 unrelated files (frameworks, print
	// filters, PDE plugins, a config-utility app) sharing the very same
	// Payload - every real PPD named "Xerox <model>.gz", no ".ppd" anywhere
	// (confirmed via *NickName content, not extension) - see
	// macSubPackagePPDFallback (macppd.go) for the shared, Ricoh-and-Xerox
	// content-based extraction fallback this needs. No Japan-market-only
	// convention found in Xerox's real data. One real, confirmed-but-dormant
	// quirk: unlike every other manufacturer here, Xerox's own *NickName
	// bakes its driver's own version string directly into the name itself
	// (e.g. "Xerox C300 Color Printer, 5.19.3") - if a technician ever keeps
	// two different Xerox driver versions side by side in the same
	// OS-version folder (the real multi-version-coexistence feature every
	// other manufacturer already supports), the same physical model from
	// each version would register as two *different* friendly model names
	// rather than two coexisting variants of one model, since
	// decorateMultiVersionLabels/BuildMacModelIndex both key on the friendly
	// model name verbatim. Not fixed here - no real file demonstrating this
	// combination exists yet (today's real Drivers folder has exactly one
	// Xerox download per OS-version folder), and stripping a
	// vendor-specific ", <version>" suffix speculatively risks the same
	// wrong-guess-without-real-data mistake Sharp's own investigation this
	// cycle was careful to avoid.
	"Xerox": {"Xerox"},

	// Toshiba ships two real driver downloads - "TOSHIBA_ColorMFP.dmg.gz" and
	// "TOSHIBA_MonoMFP.dmg.gz" (confirmed live, 2026-09-13, across every
	// populated OS-version folder in the real Drivers folder) - the
	// manufacturer's own name is always in the filename, so a single
	// "Toshiba" token unlocks the catalog-driven model index the same
	// trivial way Kyocera's/Xerox's own single tokens do, and correctly
	// classifies both downloads into the one family (real model numbers
	// never collide between the two - see below). Genuinely different real
	// PPD shape from every other manufacturer here, though: each download's
	// own sub-package (identifiers "com.toshiba.pde.x7.colormfp"/
	// "...monomfp", install-location "/") holds only 4 real PPD files total
	// each - "TOSHIBA ColorMFP"/"MonoMFP", "-X7", "-S2", "-CN" - generic
	// PDL/controller-generation variants, each one's own *NickName staying
	// just as generic regardless of which real e-STUDIO model it's serving.
	// First inspected without checking *Product (2026-09-13), which led to
	// surfacing those 8 generic names as the only selectable "models" - Ken
	// then asked whether a real e-STUDIO model number appears anywhere in
	// these PPDs at all, which led to finding the real answer: yes, each
	// file's own *Product lines (9 to 29 per file, 128 total across all 8
	// real files) name every specific e-STUDIO model that one generic PDL
	// file actually covers (e.g. e-STUDIO6570C, e-STUDIO2040C for
	// ColorMFP-X7). `toshibaExpandProductEntries` (mactoshiba.go) expands
	// each generic file-level entry into one entry per real *Product model
	// instead, so the Model dropdown now shows real e-STUDIO numbers the
	// same way every other manufacturer's own dropdown already does -
	// confirmed live that Color models always end "C"/"AC"/"CS" and Mono
	// models never do, so the two downloads' real model numbers never
	// collide even though both classify into this one shared family. Every
	// real PPD named "TOSHIBA_<Color|Mono>MFP<suffix>.gz", no ".ppd"
	// anywhere (confirmed via *NickName/*Product content, not extension) -
	// see macSubPackagePPDFallback (macppd.go) for the shared, Ricoh/Xerox/
	// Toshiba content-based extraction fallback this needs. Both real
	// downloads are themselves gzip-compressed on top of being a UDIF .dmg
	// (".dmg.gz", not a plain ".dmg") - a real shape none of
	// Canon/Kyocera/Ricoh/Sharp/Xerox's own downloads have - `hdiutil
	// attach` doesn't auto-detect a plain gzip wrapper on its own ("image
	// not recognized"), so this needed real changes to the shared mounting/
	// cataloging code itself (isDmgLikePath/mountDmg, macmount.go; the
	// extension-detection fallback, maccatalog.go), not just this table.
	// No Japan-market-only convention found in any real *Product line.
	"Toshiba": {"Toshiba"},

	// Lexmark's real download (confirmed live, 2026-09-16, Ken's own
	// "Universal_Color_Print.pkg" .dmg) is a genuine Universal Print
	// Driver: exactly ONE PPD ("Lexmark Universal Color.gz"), no per-model
	// *Product enumeration the way Toshiba/Konica Minolta's own generic
	// PDL-variant PPDs have - it auto-configures against whatever real
	// printer it talks to, rather than shipping one PPD per model. Still
	// gets a macFamilyPreference entry despite there being only one real
	// model to index: PrepareBatch's own batching only ever recognizes a
	// row once driver.MacVariantForDeploy resolves it to a real catalog
	// variant, which requires SOME model-index entry to exist - without
	// this, Lexmark would keep falling through to the old per-row path
	// forever, paying its own separate auth prompt every deploy (confirmed
	// live: exactly what happened before this existed). The real filename
	// contains "Lexmark" directly (unlike Konica Minolta), so a plain
	// manufacturer-name token works the same simple way Kyocera/Xerox/
	// Toshiba's own single tokens do.
	"Lexmark": {"Lexmark"},

	// Konica Minolta ships exactly one real driver line, but unlike every
	// other single-line manufacturer here (Kyocera/Xerox/Toshiba), no real
	// filename anywhere in the chain - not the outer .zip, not the real
	// .pkg discovered after extraction - ever contains "Konica" or
	// "Minolta" at all. Real filenames are cryptic model-code strings
	// instead (e.g. "C650i_C360i_C287i_..._MacOS_v5.2.14A.zip", eventually
	// resolving to a real .pkg named just "C750i_C287i_C4050i_C751i_C4051i_11.pkg")
	// - confirmed live (2026-09-13) across all 3 real download generations
	// in the actual Drivers folder. No manufacturer-name substring and no
	// single stable model-number substring survives across all 3
	// generations either (each ships a different lead model in its own
	// filename) - classifyMacFamily's own basename-substring convention has
	// nothing real and stable to key on. File-extension tokens are used
	// instead: every MacPackage entry scanMacPackages ever creates already
	// ends in ".zip", ".pkg", or ".dmg" by construction (that's the whole
	// scan filter), so these three reliably match any real Konica Minolta
	// package today without depending on an accidental, could-change-any-
	// time substring the way relying on e.g. "C750i" specifically would.
	// ".zip" is the one that actually matches every real download today -
	// GitHub issue #11's own fix stopped eagerly extracting a manufacturer's
	// .zip at catalog-build time, so scanMacPackages now records the outer
	// .zip itself (see MacPackageZip), never the .pkg discovered after
	// resolveMacZipSource's own on-demand extraction; ".pkg"/".dmg" are kept
	// too only as a defensive fallback should Konica Minolta ever ship one
	// of those directly, unwrapped, the way every other manufacturer here
	// sometimes does. Unlocks the catalog-driven model index the same way
	// Kyocera's/Xerox's/Toshiba's own single tokens do, just keyed on file
	// shape instead of manufacturer name. See konicaMinoltaCleanNickNames
	// (mackonicaminolta.go) for the real, needed NickName cleanup this
	// still requires: stripping a generic, non-distinguishing " PS" suffix
	// every real PPD carries, and dropping every "(S)" PPD entirely (Ken's
	// own explicit choice, 2026-09-13) - confirmed live via the real
	// package's own Localizable.strings that "(S)" means nothing more than
	// "Print (1-Sided) Driver Default" vs. the plain PPD's own "Print
	// (2-Sided) Driver Default", the exact same 30 physical models either
	// way, just a different *DefaultKMDuplex baked in - pure clutter once
	// PDT already sets its own explicit Duplex default on every queue it
	// creates regardless of which PPD variant installs. Halves the real
	// model count from 60 raw PPDs (30 plain + 30 "(S)") down to the 30
	// real, meaningfully distinct models actually worth showing. See
	// macSubPackagePPDFallback (macppd.go) for the shared, Ricoh/Xerox/
	// Toshiba/Konica-Minolta content-based extraction fallback this needs
	// (every real PPD named "KONICAMINOLTA<model>.gz", no ".ppd"
	// anywhere). No Japan-market-only convention found across the 60 raw
	// PPDs inspected.
	"Konica Minolta": {".zip", ".pkg", ".dmg"},
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
// comment already relies on for exactly this reason) - only one copy among
// them is kept as that version's own representative (preferRepresentative).
func packagesInFamily(packages []MacPackage, tokens []string, family string) []MacPackage {
	byIdentity := map[string]MacPackage{}
	for _, p := range familyCandidates(packages, tokens, family) {
		key := filepath.Base(p.Path) + "|" + strconv.FormatInt(p.Size, 10)
		if existing, ok := byIdentity[key]; !ok || preferRepresentative(p, existing) {
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

// preferRepresentative decides which of two byte-identical copies (same
// basename+size - packagesInFamily's own dedup key, confirmed the same
// logical download) of a package, found sitting in different OS-version
// folders, is kept as that download's own single representative entry -
// GitHub issue #16 follow-up (Ken's own ask, 2026-09-19): the *lowest*
// recognized OS-version folder wins when both copies parse to one, not
// whichever copy happens to have the newest file modification time.
//
// A driver package copied identically into every OS-version folder it's
// actually compatible with (this project's own established convention - see
// packagesInFamily's own doc comment) is compatible with that lowest folder
// *and every newer one* - showing that floor version (e.g. "11-BigSur") in
// a technician-facing tooltip (MacDriverCandidate.Source) conveys real,
// useful compatibility information ("works on Big Sur and up"); showing
// whichever copy simply happened to be touched most recently on disk was an
// accident of copy order, not a meaningful signal. Falls back to whichever
// copy parses to a recognized folder at all when only one does (a
// recognized floor beats an unrecognized/missing one outright, regardless
// of mtime), and to the previous newest-ModTime tie-break when neither
// parses (or both parse to the exact same rank) - the same outcome
// packagesInFamily always produced before this existed, for the case this
// project's own real Drivers folders never actually hit in practice.
func preferRepresentative(candidate, existing MacPackage) bool {
	_, candidateRecognized := osVersionFolderPrefix(candidate.OSVersionFolder)
	_, existingRecognized := osVersionFolderPrefix(existing.OSVersionFolder)
	if candidateRecognized != existingRecognized {
		return candidateRecognized
	}
	if candidateRecognized {
		if cr, er := osVersionFolderRank(candidate.OSVersionFolder), osVersionFolderRank(existing.OSVersionFolder); cr != er {
			return cr < er
		}
	}
	return candidate.ModTime.After(existing.ModTime)
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
		return &ResolvedMacPackage{Path: pkg.Path, Kind: pkg.Kind, Label: PackageLabel(pkg.Path), OSVersionFolder: pkg.OSVersionFolder}, note
	}

	return ResolveMac(catalog, manufacturer), fmt.Sprintf(
		"none of %s's driver families (%s) declare a PPD matching model %q - picked whichever package is newest instead; deploy may install the wrong driver.",
		manufacturer, strings.Join(tokens, "/"), model)
}
