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
}

// ResolveMac picks the installer package to use for manufacturer: the one
// with the newest file modification time among everything BuildMacCatalog
// found under Drivers/macOS/<manufacturer>/. Ordering by mtime rather than by
// any version parsed out of the package itself is deliberate - see
// PackageLabel's doc comment for why a macOS installer package doesn't carry
// a reliable, comparable product-version field the way a Windows .inf's
// DriverVer= line does; the file's own mtime (when it was actually placed
// under Drivers/macOS - normally when it was downloaded) is the honest signal
// available here. Returns (nil, nil) - not an error - when manufacturer has
// no local package, matching Resolve's own not-found convention.
func ResolveMac(catalog MacCatalog, manufacturer string) *ResolvedMacPackage {
	packages := catalog.Packages[manufacturer]
	if len(packages) == 0 {
		return nil
	}
	newest := packages[0]
	for _, p := range packages[1:] {
		if p.ModTime.After(newest.ModTime) {
			newest = p
		}
	}
	return &ResolvedMacPackage{Path: newest.Path, Kind: newest.Kind, Label: PackageLabel(newest.Path)}
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
	type scored struct {
		label string
		score int
	}
	var candidates []scored
	for _, path := range catalog.OpenPrintingPPDs[manufacturer] {
		label := ppdMatchLabel(path)
		total := 0
		if model != "" {
			s := FuzzyMatchScore(label, model)
			if s < 0 {
				continue
			}
			total += s
		}
		if filterText != "" {
			s := FuzzyMatchScore(label, filterText)
			if s < 0 {
				continue
			}
			total += s
		}
		candidates = append(candidates, scored{label, total})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.label
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
func OpenPrintingPPDByLabel(catalog MacCatalog, manufacturer, label string) (string, bool) {
	for _, path := range catalog.OpenPrintingPPDs[manufacturer] {
		if ppdMatchLabel(path) == label {
			return path, true
		}
	}
	return "", false
}

// ppdMatchLabel turns a PPD's own filename into the space-separated form a
// technician would actually type as a model name - see ResolveOpenPrintingPPD's
// doc comment.
func ppdMatchLabel(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasSuffix(strings.ToLower(base), ".ppd") {
		base = base[:len(base)-len(".ppd")]
	}
	return strings.ReplaceAll(base, "_", " ")
}
