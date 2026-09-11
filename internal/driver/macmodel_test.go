package driver

import (
	"os/exec"
	"testing"
)

// testModelCatalog builds the catalog from testdata_mac_model - real fixtures
// standing in for Canon's own UFR II/PostScript/"loose PPD" split, so
// BuildMacModelIndex's own family-unification and no-installer-family
// handling can be exercised without needing the user's own real,
// multi-hundred-MB Canon downloads. Two pkgbuild-built .pkg fixtures
// (UFR II, PostScript) each carry one real PPD; the third ("PPD" family) is
// a real nested-.dmg fixture with no .pkg inside at all, matching Canon's
// actual no-installer "PPD" bucket shape (confirmed live - see
// internal/driver/macmodel.go's own doc comments). "TestVendor Model X" is
// covered by all three families (UFRII: no suffix, PS: " PS" suffix, PPD:
// " PPD" suffix, per the real Canon naming convention this whole feature is
// built around); "TestVendor Model Y" exists only in the UFRII package, to
// exercise a model with just one variant. Skips on non-macOS - LocatePkg/
// LocateLoosePPDs both shell out to real macOS-only tools (pkgutil, hdiutil).
func testModelCatalog(t *testing.T) MacCatalog {
	t.Helper()
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}
	cat, err := BuildMacCatalog("testdata_mac_model")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	return cat
}

func TestBuildMacModelIndex_UnifiesModelAcrossAllThreeLanguageFamilies(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	variants := index["Canon"]["TestVendor Model X"]
	if len(variants) != 3 {
		t.Fatalf("expected 3 variants for TestVendor Model X (UFRII+PS+PPD), got %d: %+v", len(variants), variants)
	}

	byLang := map[string]MacPPDVariant{}
	for _, v := range variants {
		byLang[v.Language] = v
	}
	for _, lang := range []string{"UFRII", "PS", "PPD"} {
		if _, ok := byLang[lang]; !ok {
			t.Errorf("expected a %s variant, got none among %+v", lang, variants)
		}
	}

	if byLang["UFRII"].PackagePath == "" {
		t.Error("UFRII variant should have a PackagePath (installer-backed family)")
	}
	if byLang["PS"].PackagePath == "" {
		t.Error("PS variant should have a PackagePath (installer-backed family)")
	}
	if byLang["PPD"].PackagePath != "" {
		t.Errorf("PPD (loose) variant should have no PackagePath, got %q", byLang["PPD"].PackagePath)
	}
	if byLang["PPD"].LooseCachedPPDPath == "" {
		t.Error("PPD (loose) variant should have a LooseCachedPPDPath (no-installer family)")
	}
}

func TestBuildMacModelIndex_ModelWithOnlyOneFamilyGetsOneVariant(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	variants := index["Canon"]["TestVendor Model Y"]
	if len(variants) != 1 {
		t.Fatalf("expected exactly 1 variant for TestVendor Model Y (UFRII-only), got %d: %+v", len(variants), variants)
	}
	if variants[0].Language != "UFRII" {
		t.Errorf("expected the one variant to be UFRII, got %q", variants[0].Language)
	}
}

func TestBuildMacModelIndex_ManufacturerWithNoFamilyTableIsAbsent(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())
	if _, ok := index["Kyocera"]; ok {
		t.Error("expected no model index entry at all for a manufacturer with no macFamilyPreference table")
	}
}

func TestMacModels_RanksByFilterText(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	all := MacModels(index, "Canon", "")
	if len(all) != 2 {
		t.Fatalf("expected 2 known models, got %v", all)
	}

	narrowed := MacModels(index, "Canon", "Model Y")
	if len(narrowed) == 0 || narrowed[0] != "TestVendor Model Y" {
		t.Errorf("expected \"TestVendor Model Y\" ranked first for filterText \"Model Y\", got %v", narrowed)
	}
}

func TestMacModelCandidates_PreferenceOrderedWhenNoFilter(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	labels := MacModelCandidates(index, "Canon", "TestVendor Model X", "")
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels, got %v", labels)
	}
	want := []string{"TestVendor Model X (UFR II)", "TestVendor Model X (PostScript)", "TestVendor Model X (Generic PPD)"}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d] = %q, want %q (full list: %v)", i, labels[i], w, labels)
		}
	}
}

func TestMacModelCandidates_UnknownModelReturnsEmpty(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())
	labels := MacModelCandidates(index, "Canon", "No Such Model", "")
	if len(labels) != 0 {
		t.Errorf("expected no candidates for an unknown model, got %v", labels)
	}
}

func TestMacVariantForDeploy_ExactLabelWins(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	variant, ok := MacVariantForDeploy(index, "Canon", "TestVendor Model X", "TestVendor Model X (PostScript)")
	if !ok {
		t.Fatal("expected a match")
	}
	if variant.Language != "PS" {
		t.Errorf("expected the PS variant (explicit label commit), got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_NoDriverCommitFallsBackToPreferenceOrder(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	variant, ok := MacVariantForDeploy(index, "Canon", "TestVendor Model X", "")
	if !ok {
		t.Fatal("expected a match")
	}
	if variant.Language != "UFRII" {
		t.Errorf("expected the UFRII variant (first in Canon's own preference order), got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_ModelTypedWithDifferentCasingStillMatches(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())

	variant, ok := MacVariantForDeploy(index, "Canon", "testvendor model x", "")
	if !ok {
		t.Fatal("expected a fold-match against \"TestVendor Model X\" despite the casing difference")
	}
	if variant.Language != "UFRII" {
		t.Errorf("expected the UFRII variant, got %q", variant.Language)
	}
}

func TestMacVariantForDeploy_UnknownModelReportsNotFound(t *testing.T) {
	cat := testModelCatalog(t)
	index := BuildMacModelIndex(cat, t.TempDir())
	if _, ok := MacVariantForDeploy(index, "Canon", "Totally Unknown Model", ""); ok {
		t.Error("expected no match for a model absent from the index - caller should fall back to ResolveMacFamily/choosePPD")
	}
}
