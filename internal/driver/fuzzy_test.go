package driver

import "testing"

func TestFuzzyMatchScore(t *testing.T) {
	cases := []struct {
		text, query string
		wantMatch   bool
	}{
		{"HP Universal Printing PCL 6", "universal", true},
		{"HP Universal Printing PCL 6", "hupc6", true},  // subsequence: h-u-p-c-6 in order
		{"HP Universal Printing PCL 6", "xyz", false},
		{"Kyocera FS-1100 KX", "", true}, // empty query always scores 0 (a match)
	}
	for _, c := range cases {
		score := FuzzyMatchScore(c.text, c.query)
		gotMatch := score >= 0
		if gotMatch != c.wantMatch {
			t.Errorf("FuzzyMatchScore(%q, %q) = %d, wantMatch=%v", c.text, c.query, score, c.wantMatch)
		}
	}

	// A substring match must outrank a same-length subsequence-only match.
	sub := FuzzyMatchScore("Universal Printer", "Universal")
	seq := FuzzyMatchScore("Uxnxixvxexrxsxaxl Printer", "Universal")
	if sub <= seq {
		t.Errorf("substring match score (%d) should beat subsequence-only match score (%d)", sub, seq)
	}
}
