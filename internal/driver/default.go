package driver

import (
	"regexp"
	"strings"
	"time"
)

// defaultDriverTokens: manufacturer -> the tokens a driver name must all
// contain (case-insensitive, whitespace-insensitive, any order) to be that
// manufacturer's pre-selected default in the Defaults panel. Order-independent
// on purpose - confirmed against the real catalog that vendors aren't
// consistent about it (Sharp's own driver is named "SHARP UD3 PCL6", tokens
// in the reverse order of how "PCL 6 UD3" reads out loud), and
// whitespace-insensitive since "PCL 6" (HP) and "PCL6" (Ricoh, Sharp) both
// appear in the wild for the same two words. Deliberately excludes any
// version number embedded in a name (Xerox's and Konica Minolta's own
// preferred drivers both are - "Xerox GPD PCL6 V5.1076.4.0",
// "KONICA MINOLTA Universal PCL v3.9.13") so this keeps matching once a newer
// version replaces today's; see the tie-break note on DefaultDriverNameFor
// below for how the right one still gets picked when that leaves more than
// one name matching.
var defaultDriverTokens = map[string][]string{
	"Canon":          {"UFR", "II"},
	"HP":             {"PCL", "6"},
	"Ricoh":          {"PCL", "6"},
	"Sharp":          {"PCL", "6", "UD3"},
	"Toshiba":        {"Universal", "Printer", "2"},
	"Xerox":          {"GPD", "PCL", "6"},
	"Konica Minolta": {"Universal", "PCL"},
	"Lexmark":        {"Universal", "v2"},
}

func normalizeDriverNameForMatch(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}

// versionSuffixRe matches a dotted version number embedded in a driver's own
// display name (e.g. the "5.1076.4.0" in "Xerox GPD PCL6 V5.1076.4.0", or the
// "3.9.13" in "KONICA MINOLTA Universal PCL v3.9.13") - used only to break a
// tie between two names that otherwise match the same defaultDriverTokens and
// share the same INF-declared date, i.e. genuinely the same underlying
// driver registered under two names (see DefaultDriverNameFor).
var versionSuffixRe = regexp.MustCompile(`\d+\.\d+`)

// DefaultDriverNameFor returns the driver name to pre-select when
// manufacturer is chosen in the Defaults panel - the catalog's own name for
// the one whose name contains every token defaultDriverTokens requires for
// that manufacturer, or "" if manufacturer has no such rule or no catalog
// entry matches (e.g. the driver simply isn't installed locally).
//
// More than one name can match: a vendor's INF may register the exact same
// driver under two names (confirmed against the real Konica Minolta package -
// "KONICA MINOLTA Universal PCL" and "...Universal PCL v3.9.13" both present,
// identical date), or a vendor's preferred driver's own display name may
// embed a version number that changes over time (Xerox, Konica Minolta - see
// defaultDriverTokens), making each version a genuinely different catalog
// name rather than just a different version-group under one name. Tie-break,
// in order: newest INF-declared date wins outright; on an exact date tie
// (the same-driver-two-names case), the name that actually shows a version
// number wins, matching what a human would call "the more descriptive one";
// alphabetical is the final fallback, only to stay deterministic on a
// complete tie.
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
	var bestDate time.Time
	var bestVersioned bool
	for name, versionGroups := range mfgCatalog {
		norm := normalizeDriverNameForMatch(name)
		matchesAll := true
		for _, tok := range tokens {
			if !strings.Contains(norm, normalizeDriverNameForMatch(tok)) {
				matchesAll = false
				break
			}
		}
		if !matchesAll {
			continue
		}

		newest := newestDateInVersionGroups(versionGroups)
		versioned := versionSuffixRe.MatchString(name)

		replace := false
		switch {
		case best == "":
			replace = true
		case !newest.Equal(bestDate):
			replace = newest.After(bestDate)
		case versioned != bestVersioned:
			replace = versioned
		default:
			replace = name < best
		}
		if replace {
			best, bestDate, bestVersioned = name, newest, versioned
		}
	}
	return best
}

// newestDateInVersionGroups is the latest INF-declared date across every
// version/arch entry for one driver name.
func newestDateInVersionGroups(versionGroups map[string]map[string]ArchEntry) time.Time {
	var newest time.Time
	for _, archMap := range versionGroups {
		for _, entry := range archMap {
			if entry.Date.After(newest) {
				newest = entry.Date
			}
		}
	}
	return newest
}
