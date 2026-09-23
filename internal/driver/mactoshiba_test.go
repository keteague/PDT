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

// TestToshibaDriverHintFromFilename guards the real fix (2026-09-13, Ken's
// own finding): selecting a real model like "TOSHIBA e-STUDIO2525AC"
// populated the Driver field with "TOSHIBA e-STUDIO2525AC (Driver)" - once
// a model's own friendly name IS the real e-STUDIO number,
// languageDisplayName's generic "Driver" filler silently hid which of
// Toshiba's 4 real PDL-variant files a model's own queue actually installs
// from. The real answer, e-STUDIO2525AC -> "TOSHIBA_ColorMFP_S2.gz" ->
// "ColorMFP-S2", is copied from the real catalog build.
func TestToshibaDriverHintFromFilename(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{"TOSHIBA_ColorMFP_S2.gz", "ColorMFP-S2"},
		{"TOSHIBA_ColorMFP_X7.gz", "ColorMFP-X7"},
		{"TOSHIBA_ColorMFP_CN.gz", "ColorMFP-CN"},
		{"TOSHIBA_ColorMFP.gz", "ColorMFP"},
		{"TOSHIBA_MonoMFP_S2.gz", "MonoMFP-S2"},
		{"TOSHIBA_MonoMFP.gz", "MonoMFP"},
	}
	for _, c := range cases {
		if got := toshibaDriverHintFromFilename(c.filename); got != c.want {
			t.Errorf("toshibaDriverHintFromFilename(%q) = %q, want %q", c.filename, got, c.want)
		}
	}
}

// TestMacVariantLabel_ToshibaShowsTheRealDriverNameAlone is the end-to-end
// regression at the shared macVariantLabel level (used by the fresh-index
// path, the cached-catalog-reload path, and the multi-version-decoration
// path, so a Label reads the same everywhere) - reproduces Ken's own exact
// ask (2026-09-14): the Driver field should show exactly what macOS's own
// Printer Details shows for that queue - the real PPD's own *NickName alone
// ("TOSHIBA ColorMFP-S2"), never combined with the model name at all.
func TestMacVariantLabel_ToshibaShowsTheRealDriverNameAlone(t *testing.T) {
	if got := macVariantLabel("TOSHIBA e-STUDIO2525AC", "Toshiba", "TOSHIBA_ColorMFP_S2.gz", ""); got != "TOSHIBA ColorMFP-S2" {
		t.Errorf(`macVariantLabel(model, "Toshiba", "TOSHIBA_ColorMFP_S2.gz", "") = %q, want "TOSHIBA ColorMFP-S2" (model name must NOT appear)`, got)
	}
	// With a version tag (decorateMultiVersionLabels' own caller shape).
	if got := macVariantLabel("TOSHIBA e-STUDIO2525AC", "Toshiba", "TOSHIBA_ColorMFP_S2.gz", "2026-01-15"); got != "TOSHIBA ColorMFP-S2 (2026-01-15)" {
		t.Errorf(`macVariantLabel(..., "2026-01-15") = %q, want "TOSHIBA ColorMFP-S2 (2026-01-15)"`, got)
	}
	// Every other manufacturer (single-token family, e.g. Xerox) is
	// unaffected - still "<model> (<generic filler>)". Kyocera is the one
	// exception (Ken, 2026-09-23): its sole real family shows its actual
	// "KPDL" language, not a generic filler - see macLanguageDisplayNames.
	if got := macVariantLabel("Kyocera ECOSYS M3655idn", "Kyocera", "some_kyocera_ppd.PPD.gz", ""); got != "Kyocera ECOSYS M3655idn (KPDL)" {
		t.Errorf(`macVariantLabel(..., "Kyocera", ...) = %q, want "Kyocera ECOSYS M3655idn (KPDL)" (unaffected by the Toshiba-only fix)`, got)
	}
	// Canon's own real, genuinely distinct families keep their real names.
	if got := macVariantLabel("Canon iR-ADV C5840/5850", "UFRII", "CNPZUIRAC5840ZU.ppd.gz", ""); got != "Canon iR-ADV C5840/5850 (UFR II)" {
		t.Errorf(`macVariantLabel(..., "UFRII", ...) = %q, want "Canon iR-ADV C5840/5850 (UFR II)" (unaffected by the Toshiba-only fix)`, got)
	}
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
