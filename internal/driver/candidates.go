package driver

import (
	"sort"
	"time"
)

type candidate struct {
	Label string
	Date  time.Time
}

// Candidates ports Get-DriverCandidates: manufacturer(+model) -> selectable
// driver labels. A driver name gets one plain label unless more than one
// arch-compatible local version exists, in which case each compatible
// version gets its own decorated "<name> (vVersion - yyyy-MM-dd[, arch])"
// label so an intentionally-kept older version stays individually selectable.
func Candidates(catalog Catalog, modelIndex map[string]map[string][]string, manufacturer, model, filterText string) []string {
	mfgCatalog, ok := catalog[manufacturer]
	if !ok {
		return nil
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

	var labeled []candidate
	for _, dname := range names {
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
			labeled = append(labeled, candidate{Label: label, Date: sample.Date})
		}
	}

	if filterText != "" {
		type scored struct {
			label string
			score int
		}
		var results []scored
		for _, c := range labeled {
			s := FuzzyMatchScore(c.Label, filterText)
			if s >= 0 {
				results = append(results, scored{c.Label, s})
			}
		}
		sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
		out := make([]string, len(results))
		for i, r := range results {
			out[i] = r.label
		}
		return out
	}

	sort.SliceStable(labeled, func(i, j int) bool {
		if !labeled[i].Date.Equal(labeled[j].Date) {
			return labeled[i].Date.After(labeled[j].Date)
		}
		return labeled[i].Label < labeled[j].Label
	})
	out := make([]string, len(labeled))
	for i, c := range labeled {
		out[i] = c.Label
	}
	return out
}
