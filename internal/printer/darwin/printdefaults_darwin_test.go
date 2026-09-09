package darwin

import (
	"strings"
	"testing"
)

// Both fixture lines below are copied verbatim from `lpoptions -p
// Jenks_Kyocera -l` against a real, already-deployed Kyocera queue on this
// machine - not synthetic guesses at PPD option formatting.
const realDuplexLine = `Duplex/Duplexing: None DuplexTumble *DuplexNoTumble`
const realColorModelLine = `ColorModel/Color mode: *CMYK Gray`

func parseOneLine(t *testing.T, line string) ppdOption {
	t.Helper()
	m := ppdOptionLineRe.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("ppdOptionLineRe did not match %q", line)
	}
	return ppdOption{keyword: m[1], choices: strings.Fields(m[2])}
}

func TestPpdOptionLineRe_ParsesRealDuplexLine(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	if got.keyword != "Duplex" {
		t.Errorf("keyword = %q, want %q", got.keyword, "Duplex")
	}
	want := []string{"None", "DuplexTumble", "*DuplexNoTumble"}
	if len(got.choices) != len(want) {
		t.Fatalf("choices = %v, want %v", got.choices, want)
	}
	for i := range want {
		if got.choices[i] != want[i] {
			t.Errorf("choices[%d] = %q, want %q", i, got.choices[i], want[i])
		}
	}
}

func TestPickChoice_OneSidedPicksNoneFromRealDuplexChoices(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	choice, ok := pickChoice(got.choices, []string{"none", "simplex"}, nil)
	if !ok || choice != "None" {
		t.Errorf("pickChoice (one-sided) = %q, %v, want \"None\", true", choice, ok)
	}
}

func TestPickChoice_TwoSidedPrefersNoTumbleFromRealDuplexChoices(t *testing.T) {
	got := parseOneLine(t, realDuplexLine)
	choice, ok := pickChoice(got.choices, []string{"notumble"}, nil)
	if !ok || choice != "DuplexNoTumble" {
		t.Errorf("pickChoice (two-sided) = %q, %v, want \"DuplexNoTumble\", true", choice, ok)
	}
}

func TestPickChoice_MonoPicksGrayFromRealColorModelChoices(t *testing.T) {
	got := parseOneLine(t, realColorModelLine)
	choice, ok := pickChoice(got.choices, []string{"gray", "grey", "mono", "black"}, nil)
	if !ok || choice != "Gray" {
		t.Errorf("pickChoice (mono) = %q, %v, want \"Gray\", true", choice, ok)
	}
}

func TestPickChoice_ColorAvoidsGrayFromRealColorModelChoices(t *testing.T) {
	got := parseOneLine(t, realColorModelLine)
	choice, ok := pickChoice(got.choices, nil, []string{"gray", "grey", "mono", "black"})
	if !ok || choice != "CMYK" {
		t.Errorf("pickChoice (color) = %q, %v, want \"CMYK\", true", choice, ok)
	}
}
