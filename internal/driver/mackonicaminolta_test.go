package driver

import "testing"

func TestIsKonicaMinoltaA4RegionDir(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"A4", true},
		{"WW_A4", true},
		{"Letter", false},
		{"WW_Letter", false},
		{"a4", false}, // exact match only - confirmed live these are always cased exactly "A4"/"WW_A4"
		{"Driver", false},
	}
	for _, c := range cases {
		if got := isKonicaMinoltaA4RegionDir(c.name); got != c.want {
			t.Errorf("isKonicaMinoltaA4RegionDir(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestKonicaMinoltaCleanNickNames_StripsGenericPSSuffixAndDropsSimplexVariant
// locks in the real finding (2026-09-13) against Konica Minolta's own 60
// raw PPDs: every one carries a generic, non-distinguishing " PS" suffix
// (Konica Minolta ships no non-PostScript language variant at all) which
// gets stripped for a clean model name, and every "(S)" PPD gets dropped
// entirely - confirmed live via the real package's own Localizable.strings
// that "(S)" means nothing more than "Print (1-Sided) Driver Default" vs.
// the plain PPD's own "Print (2-Sided) Driver Default" (the exact same
// physical model either way, just a different *DefaultKMDuplex baked in) -
// pure clutter once PDT already sets its own explicit Duplex default on
// every queue it creates, Ken's own explicit choice.
func TestKonicaMinoltaCleanNickNames_StripsGenericPSSuffixAndDropsSimplexVariant(t *testing.T) {
	entries := []ppdEntry{
		{Path: "/x/KONICAMINOLTAC751i.gz", NickName: "KONICA MINOLTA C751i PS"},
		{Path: "/x/KONICAMINOLTAC3321iS.gz", NickName: "KONICA MINOLTA C3321i PS (S)"},
	}
	got := konicaMinoltaCleanNickNames(entries)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (the (S) entry must be dropped entirely): %+v", len(got), got)
	}
	if got[0].NickName != "KONICA MINOLTA C751i" {
		t.Errorf("got[0].NickName = %q, want %q", got[0].NickName, "KONICA MINOLTA C751i")
	}
	if got[0].Path != entries[0].Path {
		t.Errorf("Path must be preserved unchanged, got %+v", got)
	}
}

func TestKonicaMinoltaIsSimplexDefaultVariant(t *testing.T) {
	cases := []struct {
		nickName string
		want     bool
	}{
		{"KONICA MINOLTA C751i PS (S)", true},
		{"KONICA MINOLTA C751i PS", false},
		{"KONICA MINOLTA C751i", false},
	}
	for _, c := range cases {
		if got := konicaMinoltaIsSimplexDefaultVariant(c.nickName); got != c.want {
			t.Errorf("konicaMinoltaIsSimplexDefaultVariant(%q) = %v, want %v", c.nickName, got, c.want)
		}
	}
}

// TestClassifyMacFamily_KonicaMinoltaPkgTokenMatchesAnyRealPackage locks in
// the real finding that no real Konica Minolta filename (outer .zip or the
// real .pkg found after extraction) ever contains "Konica" or "Minolta" -
// unlike every other single-driver-line manufacturer here (Kyocera/Xerox/
// Toshiba), so the classification token can't be the manufacturer's own
// name. ".pkg" matches by construction instead - every real Konica Minolta
// package discovered across all 3 real download generations resolves down
// to a real .pkg file.
func TestClassifyMacFamily_KonicaMinoltaPkgTokenMatchesAnyRealPackage(t *testing.T) {
	tokens := macFamilyPreference["Konica Minolta"]
	for _, name := range []string{
		"C750i_C650i_C360i_C287i_C286i_C4050i_C4000i_C3320i.pkg",
		"C750i_C287i_C4050i_C751i_C4751i_11.pkg",
		"C750i_C287i_C4050i_C751i_C4051i_11.pkg",
	} {
		if got := classifyMacFamily(tokens, name); got != ".pkg" {
			t.Errorf("expected %q to classify as %q, got %q", name, ".pkg", got)
		}
	}
	if got := languageDisplayName(".pkg"); got != "Driver" {
		t.Errorf(`expected languageDisplayName(".pkg") == "Driver", got %q`, got)
	}
}
