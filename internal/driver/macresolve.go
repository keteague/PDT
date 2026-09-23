package driver

import (
	"path/filepath"
	"sort"
	"strings"
)

// ResolvedMacPackage is ResolveMac's answer: which .dmg/.pkg on disk to
// install for a manufacturer, and a best-effort display label for it (see
// PackageLabel's doc comment for why this isn't a comparable version number
// the way ResolvedDriver.Version is on Windows).
type ResolvedMacPackage struct {
	Path  string
	Kind  MacPackageKind
	Label string
	// OSVersionFolder is the real, technician-placed OS-version folder Path
	// was found under (MacPackage.OSVersionFolder's own doc comment) - used
	// by the issue #12 installer-version-gate fallback (OSVersionFolderAtLeast)
	// to decide whether that fallback is safe to attempt at all for this
	// specific package.
	OSVersionFolder string
}

// ResolveMac picks the installer package to use for manufacturer: the one
// with the newest file modification time among the current machine's own
// compatible packages found under Drivers/macOS/<manufacturer>/
// (filterToCurrentOSVersionFolder - see MacPackage.OSVersionFolder's own doc
// comment for why: PDT travels on a synced flash drive from a technician's
// own laptop to whichever client endpoint it gets plugged into next, which
// may well be on an older macOS release than the one that built the
// catalog). Ordering by mtime rather than by any version parsed out of the
// package itself is deliberate - see PackageLabel's doc comment for why a
// macOS installer package doesn't carry a reliable, comparable product-
// version field the way a Windows .inf's DriverVer= line does; the file's
// own mtime (when it was actually placed under Drivers/macOS - normally when
// it was downloaded) is the honest signal available here. Returns (nil, nil)
// - not an error - when manufacturer has no local package, matching
// Resolve's own not-found convention.
func ResolveMac(catalog MacCatalog, manufacturer string) *ResolvedMacPackage {
	packages := filterToCurrentOSVersionFolder(catalog.Packages[manufacturer])
	if len(packages) == 0 {
		return nil
	}
	newest := packages[0]
	for _, p := range packages[1:] {
		if p.ModTime.After(newest.ModTime) {
			newest = p
		}
	}
	return &ResolvedMacPackage{Path: newest.Path, Kind: newest.Kind, Label: PackageLabel(newest.Path), OSVersionFolder: newest.OSVersionFolder}
}

// OSVersionFolderForPackagePath is the same OSVersionFolder lookup
// ResolveMac already does internally, exposed for a caller that only has a
// package's own Path in hand (PrepareBatch's batched planners - see
// MacPPDVariant.PackagePath's own doc comment for why the variant itself
// doesn't carry this) rather than a freshly-resolved ResolvedMacPackage.
// Returns "" if manufacturer has no catalog entry matching packagePath at
// all - callers treat that identically to an unparsable folder name (no
// confident basis to apply the issue #12 version-gate fallback).
func OSVersionFolderForPackagePath(catalog MacCatalog, manufacturer, packagePath string) string {
	for _, p := range catalog.Packages[manufacturer] {
		if p.Path == packagePath {
			return p.OSVersionFolder
		}
	}
	return ""
}

// ResolveOpenPrintingPPD is the fallback when manufacturer has no installer
// package at all (ResolveMac returned nil): fuzzy-matches model against the
// filenames of catalog's flat Drivers/macOS/OpenPrinting/<manufacturer>/*.ppd
// bucket, reusing FuzzyMatchScore's existing ranking rather than a second
// matcher. Matches against each filename with its extension stripped and
// underscores turned into spaces, not the raw filename - confirmed necessary
// against a real OpenPrinting-style naming convention ("Ricoh_MP_C3003.ppd"):
// FuzzyMatchScore's subsequence fallback matches characters strictly in
// order, so a query like "MP C3003" (the model as a technician would type
// it, space-separated) never matches a literal underscore in the filename
// without this normalization. Returns ("", false) if manufacturer has no
// OpenPrinting PPDs, or model matches none of them (an empty model matches
// nothing here - unlike FuzzyMatchScore's own "" query always scoring 0, a
// blank model gives no basis at all to pick one PPD over another).
func ResolveOpenPrintingPPD(catalog MacCatalog, manufacturer, model string) (string, bool) {
	if model == "" {
		return "", false
	}
	candidates := catalog.OpenPrintingPPDs[manufacturer]
	bestScore := -1
	bestPath := ""
	for _, path := range candidates {
		label := ppdMatchLabel(path)
		score := FuzzyMatchScore(label, model)
		if score > bestScore {
			bestScore = score
			bestPath = path
		}
	}
	if bestScore < 0 {
		return "", false
	}
	return bestPath, true
}

// OpenPrintingCandidates is the Driver combobox's darwin data source when
// manufacturer has no installer package (see ResolveOpenPrintingPPD's own
// doc comment for the underscore/space normalization this shares with it):
// every OpenPrinting PPD label for manufacturer that matches *both* model and
// filterText (whichever of the two is non-empty - model narrows first, the
// same role it plays in Windows' own Candidates, then filterText further
// refines what's actually being typed into the Driver box live), ranked by
// the sum of their two FuzzyMatchScores. All of them, unranked-but-catalog-
// order, when both are empty - the "click to see everything available" case.
// Returns labels (see ppdMatchLabel), not raw paths - App.DriverCandidates on
// darwin hands these straight to the frontend combobox the same way
// Candidates (Windows) hands back driver names, not .inf paths.
func OpenPrintingCandidates(catalog MacCatalog, manufacturer, model, filterText string) []string {
	details := OpenPrintingCandidateDetails(catalog, manufacturer, model, filterText)
	out := make([]string, len(details))
	for i, d := range details {
		out[i] = d.Label
	}
	return out
}

// OpenPrintingCandidateDetail is one OpenPrintingCandidates entry paired
// with the real .ppd/.ppd.gz path it comes from - GitHub issue #16 follow-up
// (Ken's own ask, 2026-09-19): a technician overriding the macOS Driver
// modal's auto-picked driver needs to see which real file a candidate
// actually resolves to, not just its display label.
type OpenPrintingCandidateDetail struct {
	Label string
	Path  string
}

// OpenPrintingCandidateDetails is OpenPrintingCandidates' own richer
// sibling, returning (label, path) pairs - see that function's own doc
// comment for the shared ranking rules.
//
// Matching against model/filterText considers each PPD's own cached real
// *NickName/*ModelName content (catalog.OpenPrintingNickNames -
// BuildOpenPrintingNickNames, GitHub issue #16 follow-up, 2026-09-19), not
// just its filename-derived label - the fix for a confirmed-live, real gap:
// a real OpenPrinting PPD's own filename is routinely cryptic (e.g.
// "cnadvc5045x1g.ppd" for "Canon iR-ADV C5045/5051" - see ppdNickNameRe's
// own doc comment), so FuzzyMatchScore(label, model) alone almost never
// matched one, which is why Model became mandatory for a checked macOS row
// (GitHub issue #16's own modal) without ever meaningfully narrowing this
// list - a technician had to scroll past every OpenPrinting PPD for the
// manufacturer to find the right one. Whichever of label/nickname scores
// higher wins (bestMatchScore) - a manufacturer whose OpenPrinting PPDs
// already carry a friendly, filename-matching name (e.g. Ricoh) is
// unaffected either way.
//
// A genuinely empty result (narrowing by model matches nothing at all - no
// filename, no cached nickname) is returned as-is, not widened into every
// OpenPrinting entry for manufacturer - GitHub issue #16 follow-up (Ken's
// own explicit revision, 2026-09-19): "if there is no OpenPrinting driver
// candidate, we should not show the complete list... there simply isn't a
// candidate to choose from." A technician is never left looking at a
// silently empty dropdown regardless - macDriverCandidatesWithSource
// (macdrivercandidate.go) always offers Apple's own bundled Generic
// PostScript driver alongside whatever this returns, and darwin's own
// DriverCandidates already falls through to the same Generic set once every
// other source (this one included) comes back empty.
func OpenPrintingCandidateDetails(catalog MacCatalog, manufacturer, model, filterText string) []OpenPrintingCandidateDetail {
	return scoredOpenPrintingCandidates(catalog, manufacturer, model, filterText)
}

// bestMatchScore is the better (higher) of FuzzyMatchScore(label, text) and,
// when a real cached PPD nickname exists, FuzzyMatchScore(nickName, text) -
// -1 only when neither matches at all. See OpenPrintingCandidateDetails' own
// doc comment for why the nickname is so often the one that actually
// matches.
func bestMatchScore(text, label, nickName string) int {
	best := FuzzyMatchScore(label, text)
	if nickName == "" {
		return best
	}
	if s := FuzzyMatchScore(nickName, text); s > best {
		return s
	}
	return best
}

func scoredOpenPrintingCandidates(catalog MacCatalog, manufacturer, model, filterText string) []OpenPrintingCandidateDetail {
	type scored struct {
		detail OpenPrintingCandidateDetail
		score  int
	}
	nickNames := catalog.OpenPrintingNickNames[manufacturer]
	var candidates []scored
	for _, path := range catalog.OpenPrintingPPDs[manufacturer] {
		nickName := nickNames[path]
		// Same exclusion modelCandidatesWithSource (package main) already
		// applies to the Model dropdown's own OpenPrinting path - a
		// Japan-market-only SKU or Xerox FFPS variant has no business
		// showing up here either (confirmed live, Ken, 2026-09-23: a real
		// "RICOH IM C3510 JPN" OpenPrinting entry leaked into the Driver
		// dropdown for a Model that had already correctly excluded it).
		if IsExcludedModelVariant(nickName, path) {
			continue
		}
		label := ppdMatchLabel(path)
		total := 0
		if model != "" {
			s := bestMatchScore(model, label, nickName)
			if s < 0 {
				continue
			}
			total += s
		}
		if filterText != "" {
			s := bestMatchScore(filterText, label, nickName)
			if s < 0 {
				continue
			}
			total += s
		}
		candidates = append(candidates, scored{OpenPrintingCandidateDetail{Label: label, Path: path}, total})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	out := make([]OpenPrintingCandidateDetail, len(candidates))
	for i, c := range candidates {
		out[i] = c.detail
	}
	return out
}

// OpenPrintingPPDByLabel looks up an exact OpenPrintingCandidates label (what
// a technician actually picked from the Driver dropdown, e.g.
// row.Driver after committing a candidate - see setupCombobox's own commit()
// in main.js) back to its real PPD path. Deploy-time resolution
// (deploy_darwin.go) prefers this over re-deriving a PPD from Model's own
// fuzzy match whenever the technician explicitly picked one - a real
// selection is always more authoritative than a best guess.
//
// Tries the filename-derived label (ppdMatchLabel) first, then falls back
// to matching against the PPD's own cached real *NickName/*ModelName
// content (catalog.OpenPrintingNickNames) - GitHub issue #16 follow-up
// (Ken's own ask, 2026-09-19): the macOS Driver modal's own dropdown now
// shows that NickName as the primary label whenever one is cached
// (macDriverCandidatesWithSource, package main), so a saved MacDriver
// commitment can be any of three forms depending on when it was picked: the
// original filename-derived form (ppdMatchLabel, always "(OP)"-suffixed -
// an older saved config, or a manufacturer/PPD BuildOpenPrintingNickNames
// hasn't cataloged yet), a bare cached NickName with no suffix at all (a
// config saved between the #16 follow-up and the "(OP)" tag being restored
// onto it, Ken, 2026-09-23), or a NickName with "(OP)" now appended (every
// config saved after that fix) - the nickName+" (OP)" check below matches
// exactly what openPrintingCandidateWithSource (package main) hands the
// dropdown today, but nickName alone is kept so nothing saved during that
// window ever silently stops resolving.
func OpenPrintingPPDByLabel(catalog MacCatalog, manufacturer, label string) (string, bool) {
	for _, path := range catalog.OpenPrintingPPDs[manufacturer] {
		if ppdMatchLabel(path) == label {
			return path, true
		}
	}
	for _, path := range catalog.OpenPrintingPPDs[manufacturer] {
		if nick, ok := catalog.OpenPrintingNickNames[manufacturer][path]; ok && nick != "" && (nick == label || nick+" (OP)" == label) {
			return path, true
		}
	}
	return "", false
}

// ppdMatchLabel turns a PPD's own filename into the space-separated form a
// technician would actually type as a model name - see ResolveOpenPrintingPPD's
// doc comment - with a trailing " (OP)" marking it as coming from the
// OpenPrinting fallback bucket (community-maintained generic PPDs, never a
// real vendor-branded driver package) rather than a real installed driver -
// Ken's own explicit ask (2026-09-16), after a real Lexmark deploy used one
// of these without anything in the Driver dropdown distinguishing it from a
// genuine Lexmark driver. The single source of truth for this label - both
// OpenPrintingCandidates (building the dropdown) and OpenPrintingPPDByLabel
// (resolving a technician's own saved/committed pick back to a real PPD
// path) call this same function, so the suffix round-trips correctly
// without either side needing to know about it specially. Appending it here
// (not just when displaying) does mean it's folded into the fuzzy-match text
// FuzzyMatchScore ranks candidates by too - harmless, since every OpenPrinting
// candidate carries the exact same suffix, so relative ranking between them
// is unaffected.
func ppdMatchLabel(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasSuffix(strings.ToLower(base), ".ppd") {
		base = base[:len(base)-len(".ppd")]
	}
	return strings.ReplaceAll(base, "_", " ") + " (OP)"
}
