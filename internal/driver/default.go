package driver

import "strings"

// defaultDriverTokens: manufacturer -> the tokens a driver name must all
// contain (case-insensitive, whitespace-insensitive, any order) to be that
// manufacturer's pre-selected default in the Defaults panel. Order-independent
// on purpose - confirmed against the real catalog that vendors aren't
// consistent about it (Sharp's own driver is named "SHARP UD3 PCL6", tokens
// in the reverse order of how "PCL 6 UD3" reads out loud), and
// whitespace-insensitive since "PCL 6" (HP) and "PCL6" (Ricoh, Sharp) both
// appear in the wild for the same two words.
var defaultDriverTokens = map[string][]string{
	"Canon": {"UFR", "II"},
	"HP":    {"PCL", "6"},
	"Ricoh": {"PCL", "6"},
	"Sharp": {"PCL", "6", "UD3"},
}

func normalizeDriverNameForMatch(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}

// DefaultDriverNameFor returns the driver name to pre-select when
// manufacturer is chosen in the Defaults panel - the catalog's own name for
// the one whose name contains every token defaultDriverTokens requires for
// that manufacturer, or "" if manufacturer has no such rule or no catalog
// entry matches (e.g. the driver simply isn't installed locally). If more
// than one name somehow matches, the alphabetically-first is returned, for a
// deterministic result rather than depending on Go's random map iteration
// order.
func DefaultDriverNameFor(catalog Catalog, manufacturer string) string {
	tokens, ok := defaultDriverTokens[manufacturer]
	if !ok {
		return ""
	}
	mfgCatalog, ok := catalog[manufacturer]
	if !ok {
		return ""
	}

	var best string
	for name := range mfgCatalog {
		norm := normalizeDriverNameForMatch(name)
		matchesAll := true
		for _, tok := range tokens {
			if !strings.Contains(norm, normalizeDriverNameForMatch(tok)) {
				matchesAll = false
				break
			}
		}
		if matchesAll && (best == "" || name < best) {
			best = name
		}
	}
	return best
}
