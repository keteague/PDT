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

// TestKonicaMinoltaCleanNickNames_StripsGenericPSSuffixButKeepsRealSVariant
// locks in the real finding (2026-09-13) against Konica Minolta's own 60
// real PPDs: every one carries a generic, non-distinguishing " PS" suffix
// (Konica Minolta ships no non-PostScript language variant at all) which
// should be stripped for a clean model name, but a trailing "(S)" qualifier
// some models also carry names a real, separately-installable driver
// variant (that package's own second, non-default sub-package) and must
// survive - collapsing it away would silently merge two real driver
// variants under the same friendly model name.
func TestKonicaMinoltaCleanNickNames_StripsGenericPSSuffixButKeepsRealSVariant(t *testing.T) {
	entries := []ppdEntry{
		{Path: "/x/KONICAMINOLTAC751i.gz", NickName: "KONICA MINOLTA C751i PS"},
		{Path: "/x/KONICAMINOLTAC3321iS.gz", NickName: "KONICA MINOLTA C3321i PS (S)"},
	}
	got := konicaMinoltaCleanNickNames(entries)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].NickName != "KONICA MINOLTA C751i" {
		t.Errorf("got[0].NickName = %q, want %q", got[0].NickName, "KONICA MINOLTA C751i")
	}
	if got[1].NickName != "KONICA MINOLTA C3321i (S)" {
		t.Errorf("got[1].NickName = %q, want %q (the real (S) variant must survive)", got[1].NickName, "KONICA MINOLTA C3321i (S)")
	}
	// Path must be untouched - both entries still point at their own real
	// underlying PPD file, nothing shared/renamed.
	if got[0].Path != entries[0].Path || got[1].Path != entries[1].Path {
		t.Errorf("Path must be preserved unchanged, got %+v", got)
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
