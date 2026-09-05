package driver

import (
	"regexp"
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
