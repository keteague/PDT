package driver

import (
	"os/exec"
	"testing"
)

func TestResolveMac_PicksNewestByModTime(t *testing.T) {
	cat := testMacCatalog(t)
	got := ResolveMac(cat, "Canon")
	if got == nil {
		t.Fatal("ResolveMac returned nil, want the newer Canon package")
	}
	if got.Kind != MacPackagePkg {
		t.Errorf("expected the newer (2024-06) .pkg to win over the older (2024-01) .dmg, got Kind=%v Path=%s", got.Kind, got.Path)
	}
}

func TestResolveMac_UnknownManufacturerReturnsNil(t *testing.T) {
	cat := testMacCatalog(t)
	if got := ResolveMac(cat, "Ricoh"); got != nil {
		t.Errorf("ResolveMac(Ricoh) = %v, want nil (Ricoh has only OpenPrinting PPDs, no installer package)", got)
	}
}

func TestResolveMac_LabelFromRealPackageInfo(t *testing.T) {
	// CanonDriverNewer.pkg is a real flat .pkg fixture (built via pkgbuild
	// --version 1.2.3) - proves PackageLabel actually reads a flat package's
	// own PackageInfo rather than always falling back to the filename.
	// PackageLabel shells out to the real macOS pkgutil binary (see
	// macmount.go) - skip rather than fail when it's not on PATH, i.e. on
	// every non-macOS dev machine, where PackageLabel can only ever exercise
	// its filename fallback.
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS) - cannot verify real PackageInfo parsing")
	}
	cat := testMacCatalog(t)
	got := ResolveMac(cat, "Canon")
	if got == nil {
		t.Fatal("ResolveMac returned nil")
	}
	if got.Label != "1.2.3" {
		t.Errorf("Label = %q, want %q (from the fixture's real PackageInfo)", got.Label, "1.2.3")
	}
}

func TestResolveOpenPrintingPPD_FuzzyMatchesModel(t *testing.T) {
	cat := testMacCatalog(t)
	path, ok := ResolveOpenPrintingPPD(cat, "Ricoh", "MP C3003")
	if !ok {
		t.Fatal("ResolveOpenPrintingPPD returned not-found, want a fuzzy match against Ricoh_MP_C3003.ppd")
	}
	if got := path; got == "" {
		t.Error("expected a non-empty path")
	}
}

func TestResolveOpenPrintingPPD_NoManufacturerMatch(t *testing.T) {
	cat := testMacCatalog(t)
	if _, ok := ResolveOpenPrintingPPD(cat, "Canon", "anything"); ok {
		t.Error("expected not-found for a manufacturer with no OpenPrinting PPDs")
	}
}

func TestResolveOpenPrintingPPD_EmptyModelNoMatch(t *testing.T) {
	cat := testMacCatalog(t)
	if _, ok := ResolveOpenPrintingPPD(cat, "Ricoh", ""); ok {
		t.Error("expected not-found for an empty model - nothing to base a match on")
	}
}

func TestOpenPrintingCandidates_EmptyQueriesReturnsEverything(t *testing.T) {
	cat := testMacCatalog(t)
	got := OpenPrintingCandidates(cat, "Ricoh", "", "")
	if len(got) != 2 {
		t.Fatalf("expected both Ricoh OpenPrinting PPDs with no query, got %v", got)
	}
}

func TestOpenPrintingCandidates_ModelNarrows(t *testing.T) {
	cat := testMacCatalog(t)
	got := OpenPrintingCandidates(cat, "Ricoh", "MP C3003", "")
	if len(got) != 1 || got[0] != "Ricoh MP C3003" {
		t.Errorf("OpenPrintingCandidates(model=%q) = %v, want just [%q]", "MP C3003", got, "Ricoh MP C3003")
	}
}

func TestOpenPrintingCandidates_ModelAndFilterTextBothMustMatch(t *testing.T) {
	cat := testMacCatalog(t)
	// "MP C3003" matches only the Ricoh MP C3003 PPD; a filterText that
	// doesn't also match it (nothing in that label resembles "zzz") should
	// exclude it even though model alone would have matched.
	got := OpenPrintingCandidates(cat, "Ricoh", "MP C3003", "zzz-no-such-text")
	if len(got) != 0 {
		t.Errorf("OpenPrintingCandidates(model matches, filterText doesn't) = %v, want none", got)
	}
}
