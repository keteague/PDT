package driver

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempPPD writes content (plain text, not gzipped - ReadPPDProducts
// only gzip-decompresses a ".gz"-suffixed path, and a plain-text PPD is
// exactly as readable either way) to dir/name, returning its path.
func writeTempPPD(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test PPD %s: %v", path, err)
	}
	return path
}

func TestToshibaCanonicalModelName(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// Real *Product lines confirmed live (2026-09-13) against Toshiba's
		// own real ColorMFP-X7/MonoMFP.gz PPDs.
		{"TOSHIBA e-STUDIO6570C", "TOSHIBA e-STUDIO6570C"},
		{"TOSHIBA e-STUDIO856", "TOSHIBA e-STUDIO856"},
		{"TOSHIBA e-STUDIO5008LP_Loops-LP50", "TOSHIBA e-STUDIO5008LP Loops-LP50"},
		{"TOSHIBA e-STUDIO5008LP Loops-LP50", "TOSHIBA e-STUDIO5008LP Loops-LP50"},
		{"e-STUDIO5008LP_Loops-LP50", "TOSHIBA e-STUDIO5008LP Loops-LP50"},
		{"Loops-LP30_e-STUDIO306LP", "TOSHIBA Loops-LP30 e-STUDIO306LP"},
	}
	for _, c := range cases {
		if got := toshibaCanonicalModelName(c.raw); got != c.want {
			t.Errorf("toshibaCanonicalModelName(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

// TestToshibaCanonicalModelName_DeduplicatesAcrossRawSpellingVariants guards
// the real finding that motivated toshibaCanonicalModelName in the first
// place: the same real model shows up as 3 differently-spelled raw
// *Product lines (underscore vs space, with/without a "TOSHIBA " prefix) -
// all three must normalize to the exact same string, or
// toshibaExpandProductEntries' own dedup (a plain string-keyed set) would
// register 3 near-duplicate models instead of 1.
func TestToshibaCanonicalModelName_DeduplicatesAcrossRawSpellingVariants(t *testing.T) {
	variants := []string{
		"TOSHIBA e-STUDIO5008LP_Loops-LP50",
		"TOSHIBA e-STUDIO5008LP Loops-LP50",
		"e-STUDIO5008LP_Loops-LP50",
	}
	first := toshibaCanonicalModelName(variants[0])
	for _, v := range variants[1:] {
		if got := toshibaCanonicalModelName(v); got != first {
			t.Errorf("toshibaCanonicalModelName(%q) = %q, want %q (same as %q)", v, got, first, variants[0])
		}
	}
}

// realToshibaColorX7ProductBlock is copied verbatim-shaped (2026-09-13) from
// a representative slice of the real TOSHIBA_ColorMFP_X7.gz PPD's own
// *Product lines (22 real lines in the actual file - trimmed here to a
// representative sample covering the real dedup/prefix cases).
const realToshibaColorX7ProductBlock = `*Product: "(TOSHIBA e-STUDIO6570C)"
*Product: "(TOSHIBA e-STUDIO6560C)"
*Product: "(TOSHIBA e-STUDIO2040C)"
*NickName: "TOSHIBA ColorMFP-X7"
`

func TestToshibaExpandProductEntries_ExpandsOneFileIntoManyRealModels(t *testing.T) {
	dir := t.TempDir()
	// Plain text, not gzipped - so the fixture filename deliberately does
	// NOT end in ".gz" (readPPDTextBytes would otherwise try to gunzip this
	// plain text and fail).
	path := writeTempPPD(t, dir, "TOSHIBA_ColorMFP_X7.ppd", realToshibaColorX7ProductBlock)

	entries := []ppdEntry{{Path: path, NickName: "TOSHIBA ColorMFP-X7"}}
	expanded := toshibaExpandProductEntries(entries)

	want := map[string]bool{
		"TOSHIBA e-STUDIO6570C": true,
		"TOSHIBA e-STUDIO6560C": true,
		"TOSHIBA e-STUDIO2040C": true,
	}
	if len(expanded) != len(want) {
		t.Fatalf("expanded = %+v, want %d entries matching %v", expanded, len(want), want)
	}
	for _, e := range expanded {
		if !want[e.NickName] {
			t.Errorf("unexpected expanded model %q", e.NickName)
		}
		if e.Path != path {
			t.Errorf("expanded entry %q has Path %q, want the original file's own path %q (every model shares the same real PPD file)", e.NickName, e.Path, path)
		}
	}
}

// TestToshibaExpandProductEntries_FallsBackWhenNoProductLines guards that a
// file declaring no *Product line at all (never actually observed live
// against Toshiba's real 8 files, but not something to silently lose data
// over) keeps its own original, generic-NickName entry rather than
// disappearing entirely.
func TestToshibaExpandProductEntries_FallsBackWhenNoProductLines(t *testing.T) {
	dir := t.TempDir()
	path := writeTempPPD(t, dir, "TOSHIBA_ColorMFP_NoProduct.ppd", "*NickName: \"TOSHIBA ColorMFP-Generic\"\n")

	entries := []ppdEntry{{Path: path, NickName: "TOSHIBA ColorMFP-Generic"}}
	expanded := toshibaExpandProductEntries(entries)

	if len(expanded) != 1 || expanded[0].NickName != "TOSHIBA ColorMFP-Generic" {
		t.Errorf("expanded = %+v, want the original unexpanded entry kept", expanded)
	}
}
