package driver

import (
	"regexp"
	"sort"
	"strings"
)

var (
	kyoceraPrefixRe = regexp.MustCompile(`(?i)^Kyocera\s+`)
	kyoceraSuffixRe = regexp.MustCompile(`(?i)\s+KX$`)
)

// ModelFromDriverName ports Get-ModelFromDriverName: Kyocera driver names
// embed the model between a "Kyocera " prefix and a " KX" suffix (e.g.
// "Kyocera TASKalfa 8353ci KX" -> "TASKalfa 8353ci"); other manufacturers'
// driver names aren't model-specific, so they return "".
func ModelFromDriverName(manufacturer, driverName string) string {
	if manufacturer != "Kyocera" {
		return ""
	}
	s := kyoceraPrefixRe.ReplaceAllString(driverName, "")
	s = kyoceraSuffixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// BuildModelIndex ports Build-ModelIndex: Manufacturer -> Model -> driver
// names, populated only where ModelFromDriverName resolves a non-empty model.
func BuildModelIndex(catalog Catalog) map[string]map[string][]string {
	index := map[string]map[string][]string{}
	for mfg, drivers := range catalog {
		index[mfg] = map[string][]string{}
		for dname := range drivers {
			model := ModelFromDriverName(mfg, dname)
			if model == "" {
				continue
			}
			index[mfg][model] = append(index[mfg][model], dname)
		}
	}
	return index
}

// Models lists manufacturer's known models from modelIndex (see
// BuildModelIndex - Kyocera only today, since other manufacturers' driver
// names aren't model-specific), ranked by FuzzyMatchScore against filterText
// the same way Candidates ranks driver names. Alphabetical when filterText
// is empty, since every candidate then ties at score 0. []string{}, not nil,
// for a manufacturer with no model data at all - a nil slice marshals to
// JSON `null`, and the grid row's own Model field combobox calls this with
// no defensive fallback (same class of bug ManufacturersWithDrivers' own
// comment already documents) - []string{} is what tells that combobox "no
// lookup available, leave this a plain free-text input" without crashing.
func Models(modelIndex map[string]map[string][]string, manufacturer, filterText string) []string {
	byModel := modelIndex[manufacturer]
	type scored struct {
		name  string
		score int
	}
	candidates := make([]scored, 0, len(byModel))
	for m := range byModel {
		score := 0
		if filterText != "" {
			if score = FuzzyMatchScore(m, filterText); score < 0 {
				continue
			}
		}
		candidates = append(candidates, scored{m, score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].name < candidates[j].name
	})
	models := make([]string, len(candidates))
	for i, c := range candidates {
		models[i] = c.name
	}
	return models
}
