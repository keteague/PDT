package driver

import (
	"regexp"
	"sort"
	"time"
)

// faxDriverNameRe excludes fax "drivers" from the Windows Driver candidate
// list - PDT only ever deploys print queues, never fax queues, so a fax
// driver name (Konica Minolta's own real data ships "KONICA MINOLTA
// Universal FAX" alongside its real PCL/PS drivers, confirmed 2026-09-21) is
// never a usable answer here. Matched as a whole word so a hypothetical
// print driver whose own name merely contains "fax" as a substring - none
// observed in any real catalog inspected so far - wouldn't be caught by
// accident.
var faxDriverNameRe = regexp.MustCompile(`(?i)\bfax\b`)

// trailingVDottedVersionRe matches a trailing " vX.Y[.Z...]" version suffix
// some vendors' own .inf files append to an otherwise-identical driver name -
// Konica Minolta's own real data (confirmed 2026-09-21) declares the exact
// same underlying driver under both "KONICA MINOLTA Universal PCL" and
// "KONICA MINOLTA Universal PCL v3.9.13" as two separate Model entries
// inside the identical .inf, both resolving to the identical installed
// files. See bareNameDuplicates. Distinct from default.go's own
// versionSuffixRe (a looser "does this name contain a version number
// anywhere" check, unanchored) - this one specifically anchors to the end
// of the string with a leading " v", the exact shape a name/name-pair needs
// to be treated as the same driver declared twice.
var trailingVDottedVersionRe = regexp.MustCompile(`^(.+) v[0-9]+(?:\.[0-9]+)*$`)

// bareNameDuplicates returns the set of driver names in mfgCatalog that
// should be dropped from the candidate list because a version-suffixed
// sibling name exists sharing at least one identical InfPath - Ken's own
// explicit ask (2026-09-21): "Only show the one that has a driver version
// number in it." Keying off a shared InfPath, not just the name match,
// means two genuinely different drivers that merely happen to share a name
// prefix are never conflated into one.
func bareNameDuplicates(mfgCatalog map[string]map[string]map[string]ArchEntry) map[string]bool {
	infPaths := func(dname string) map[string]bool {
		set := map[string]bool{}
		for _, byArch := range mfgCatalog[dname] {
			for _, e := range byArch {
				if e.InfPath != "" {
					set[e.InfPath] = true
				}
			}
		}
		return set
	}
	suppressed := map[string]bool{}
	for dname := range mfgCatalog {
		m := trailingVDottedVersionRe.FindStringSubmatch(dname)
		if m == nil {
			continue
		}
		bareName := m[1]
		if _, ok := mfgCatalog[bareName]; !ok {
			continue
		}
		versionedPaths := infPaths(dname)
		for p := range infPaths(bareName) {
			if versionedPaths[p] {
				suppressed[bareName] = true
				break
			}
		}
	}
	return suppressed
}

type candidate struct {
	Label   string
	Date    time.Time
	Sources []string
}

// CandidateDetail is one Candidates entry paired with the real package(s) it
// comes from - the archive (.zip/.msi/self-extracting .exe) when one exists,
// else the extracted .inf itself. Sources is sorted and de-duplicated (a
// single label can span an x64 and an arm64 package). Used by the Windows
// Driver dropdown's tooltip so a technician choosing between several
// similarly-named drivers can see which manufacturer folder and package each
// one actually comes from.
type CandidateDetail struct {
	Label   string
	Sources []string
}

// Candidates ports Get-DriverCandidates: manufacturer(+model) -> selectable
// driver labels. A driver name gets one plain label unless more than one
// arch-compatible local version exists, in which case each compatible
// version gets its own decorated "<name> (vVersion - yyyy-MM-dd[, arch])"
// label so an intentionally-kept older version stays individually selectable.
func Candidates(catalog Catalog, modelIndex map[string]map[string][]string, manufacturer, model, filterText string) []string {
	details := CandidateDetails(catalog, modelIndex, manufacturer, model, filterText)
	out := make([]string, len(details))
	for i, d := range details {
		out[i] = d.Label
	}
	return out
}

// CandidateDetails is Candidates' own richer sibling - identical ordering
// and filtering, but each label also carries its source package path(s).
func CandidateDetails(catalog Catalog, modelIndex map[string]map[string][]string, manufacturer, model, filterText string) []CandidateDetail {
	mfgCatalog, ok := catalog[manufacturer]
	if !ok {
		// []string{}, not nil - a nil slice marshals to JSON `null`, and the
		// Driver combobox's own render() calls .map() on whatever this
		// resolves to with no defensive fallback (see
		// ManufacturersWithDrivers' own comment for the exact same class of
		// bug) - an unrecognized/not-yet-selected manufacturer (blank, in
		// particular, when zero manufacturers have any drivers at all) is a
		// routine "nothing to offer yet" result, not an error.
		return []CandidateDetail{}
	}

	var names []string
	if model != "" {
		if byModel, ok := modelIndex[manufacturer]; ok {
			if list, ok := byModel[model]; ok {
				names = list
			}
		}
	}
	if names == nil {
		for dname := range mfgCatalog {
			names = append(names, dname)
		}
	}

	suppressed := bareNameDuplicates(mfgCatalog)

	var labeled []candidate
	for _, dname := range names {
		if suppressed[dname] || faxDriverNameRe.MatchString(dname) {
			continue
		}
		versionGroups := mfgCatalog[dname]
		var compatibleKeys []string
		for vkey, archMap := range versionGroups {
			if archMapCompatible(archMap) {
				compatibleKeys = append(compatibleKeys, vkey)
			}
		}
		if len(compatibleKeys) == 0 {
			continue
		}

		multiVersion := len(compatibleKeys) > 1
		for _, vkey := range compatibleKeys {
			archesInGroup := versionGroups[vkey]
			var sample ArchEntry
			for _, v := range archesInGroup {
				sample = v
				break
			}
			label := dname
			if multiVersion {
				label = dname + " (v" + sample.Version + " - " + sample.Date.Format("2006-01-02")
				if len(archesInGroup) == 1 {
					for arch := range archesInGroup {
						label += ", " + arch
					}
				}
				label += ")"
			}
			seen := map[string]bool{}
			var sources []string
			for _, e := range archesInGroup {
				src := e.ArchivePath
				if src == "" {
					src = e.InfPath
				}
				if src != "" && !seen[src] {
					seen[src] = true
					sources = append(sources, src)
				}
			}
			sort.Strings(sources)
			labeled = append(labeled, candidate{Label: label, Date: sample.Date, Sources: sources})
		}
	}

	if filterText != "" {
		type scored struct {
			c     candidate
			score int
		}
		var results []scored
		for _, c := range labeled {
			s := FuzzyMatchScore(c.Label, filterText)
			if s >= 0 {
				results = append(results, scored{c, s})
			}
		}
		sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
		out := make([]CandidateDetail, len(results))
		for i, r := range results {
			out[i] = CandidateDetail{Label: r.c.Label, Sources: r.c.Sources}
		}
		return out
	}

	sort.SliceStable(labeled, func(i, j int) bool {
		if !labeled[i].Date.Equal(labeled[j].Date) {
			return labeled[i].Date.After(labeled[j].Date)
		}
		return labeled[i].Label < labeled[j].Label
	})
	out := make([]CandidateDetail, len(labeled))
	for i, c := range labeled {
		out[i] = CandidateDetail{Label: c.Label, Sources: c.Sources}
	}
	return out
}
