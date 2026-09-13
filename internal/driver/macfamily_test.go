package driver

import (
	"os/exec"
	"strings"
	"testing"
)

// testFamilyCatalog builds the catalog from testdata_mac_family - three real
// flat .pkg fixtures (built via pkgbuild, each with one real PPD carrying its
// own distinct *NickName) standing in for Canon's real UFRII/PS/PPD family
// split, so ResolveMacFamily's preference-order and fallback logic can be
// exercised without needing the user's own real, multi-hundred-MB Canon
// downloads. Skips on non-macOS, same reasoning as macresolve_test.go's own
// pkgutil-dependent tests - PackageBestModelScore shells out to the real
// pkgutil binary.
func testFamilyCatalog(t *testing.T) MacCatalog {
	t.Helper()
	if _, err := exec.LookPath("pkgutil"); err != nil {
		t.Skip("pkgutil not on PATH (not running on macOS)")
	}
	cat, err := BuildMacCatalog("testdata_mac_family")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	return cat
}

func TestResolveMacFamily_PreferredFamilyMatchesDirectly(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, note := ResolveMacFamily(cat, "Canon", "Test Model A")
	if resolved == nil {
		t.Fatal("expected a resolved package")
	}
	if !strings.Contains(resolved.Path, "UFRII_test_fixture") {
		t.Errorf("expected the UFRII fixture (preferred family, has a real match), got %s", resolved.Path)
	}
	if note != "" {
		t.Errorf("expected no note when the preferred family matches directly, got %q", note)
	}
}

func TestResolveMacFamily_FallsBackToSecondPreferenceWithNote(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, note := ResolveMacFamily(cat, "Canon", "Test Model B")
	if resolved == nil {
		t.Fatal("expected a resolved package")
	}
	if !strings.Contains(resolved.Path, "PS_test_fixture") {
		t.Errorf("expected the PS fixture (UFRII has no match, PS does), got %s", resolved.Path)
	}
	if note == "" {
		t.Error("expected a note explaining the fallback from the preferred UFRII family to PS")
	}
}

func TestResolveMacFamily_FallsBackToThirdPreferenceWithNote(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, note := ResolveMacFamily(cat, "Canon", "Test Model C")
	if resolved == nil {
		t.Fatal("expected a resolved package")
	}
	if !strings.Contains(resolved.Path, "PPD_test_fixture") {
		t.Errorf("expected the PPD fixture (only family with a match for Model C), got %s", resolved.Path)
	}
	if note == "" {
		t.Error("expected a note explaining the fallback to the least-preferred PPD family")
	}
}

func TestResolveMacFamily_NoFamilyMatchesFallsBackToNewestWithWarningNote(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, note := ResolveMacFamily(cat, "Canon", "Totally Unknown Model Z9000")
	if resolved == nil {
		t.Fatal("expected a resolved package (falls back to newest-overall even with no confident match)")
	}
	if note == "" {
		t.Error("expected a note warning that no family's PPDs matched the model")
	}
}

func TestResolveMacFamily_BlankModelFallsBackWithNote(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, note := ResolveMacFamily(cat, "Canon", "")
	if resolved == nil {
		t.Fatal("expected a resolved package")
	}
	if note == "" {
		t.Error("expected a note explaining that a blank model can't be checked against the family preference order")
	}
}

func TestResolveMacFamily_ManufacturerWithNoFamilyTableBehavesLikeResolveMac(t *testing.T) {
	cat := testMacCatalog(t) // the plain (non-family) fixture from macresolve_test.go
	// "Ricoh", not "Kyocera" - Kyocera got its own single-token
	// macFamilyPreference entry (see macfamily.go's own doc comment) once it
	// got a real model index built, so it's no longer a valid example of "a
	// manufacturer with no family table at all". Ricoh has no installer-
	// package fixture either (only an OpenPrinting bucket one, unrelated to
	// catalog.Packages) and no macFamilyPreference entry, so both calls
	// below should agree there's nothing to resolve - both nil, no note -
	// purely from ResolveMacFamily's own len(tokens)==0 short-circuit, never
	// reaching the family-matching logic at all.
	resolved, note := ResolveMacFamily(cat, "Ricoh", "anything")
	if note != "" {
		t.Errorf("expected no note for a manufacturer with no family table, got %q", note)
	}
	plain := ResolveMac(cat, "Ricoh")
	if resolved != nil || plain != nil {
		t.Errorf("expected both ResolveMacFamily and ResolveMac to return nil for a manufacturer with no installer package at all: got %v vs %v", resolved, plain)
	}
}
