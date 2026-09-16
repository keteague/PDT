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
	disableOSVersionFiltering(t)
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

// TestResolveMacFamily_PopulatesOSVersionFolder guards a real gap found
// live (2026-09-16, issue #12): this branch's own ResolvedMacPackage was
// built by hand without setting OSVersionFolder at all, unlike ResolveMac's
// own construction - silently defeating the installer-version-gate
// fallback (OSVersionFolderAtLeast) for any manufacturer with a
// macFamilyPreference entry whose model actually matches a family (as
// opposed to the model-blank/no-match branches, which both already
// delegate to ResolveMac and so were already correct).
func TestResolveMacFamily_PopulatesOSVersionFolder(t *testing.T) {
	cat := testFamilyCatalog(t)
	resolved, _ := ResolveMacFamily(cat, "Canon", "Test Model A")
	if resolved == nil {
		t.Fatal("expected a resolved package")
	}
	if resolved.OSVersionFolder != "26-Tahoe" {
		t.Errorf("OSVersionFolder = %q, want %q (the matched fixture's own real folder)", resolved.OSVersionFolder, "26-Tahoe")
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
	// "HP", not "Sharp" - Sharp got its own real macFamilyPreference entry
	// once it got a real model index built (see macfamily.go's own doc
	// comment: "MacPS"/"PPD"), so it's no longer a valid example of "a
	// manufacturer with no family table at all". HP has no installer-package
	// fixture and no macFamilyPreference entry, so both calls below should
	// agree there's nothing to resolve - both nil, no note - purely from
	// ResolveMacFamily's own len(tokens)==0 short-circuit, never reaching the
	// family-matching logic at all.
	resolved, note := ResolveMacFamily(cat, "HP", "anything")
	if note != "" {
		t.Errorf("expected no note for a manufacturer with no family table, got %q", note)
	}
	plain := ResolveMac(cat, "HP")
	if resolved != nil || plain != nil {
		t.Errorf("expected both ResolveMacFamily and ResolveMac to return nil for a manufacturer with no installer package at all: got %v vs %v", resolved, plain)
	}
}

// TestClassifyMacFamily_SharpMacPSTokenMatchesRealDriverNotTheDecoy locks in
// the real, live-verified finding (2026-09-13) behind macFamilyPreference's
// own "Sharp" entry: unlike Kyocera, Sharp's real driver filename
// ("MX-C55c_2512a_MacPS.dmg") never contains the manufacturer's own name, so
// "MacPS" is the token that actually does the job - and it must never match
// "Generic_GUC_PrinterSoftware_11202025.dmg", the real, zero-PPD Lexmark-
// licensed decoy file that was wrongly winning ResolveMac's plain
// newest-by-mtime fallback before this table existed.
func TestClassifyMacFamily_SharpMacPSTokenMatchesRealDriverNotTheDecoy(t *testing.T) {
	tokens := macFamilyPreference["Sharp"]
	if got := classifyMacFamily(tokens, "MX-C55c_2512a_MacPS.dmg"); got != "MacPS" {
		t.Errorf("expected the real Sharp driver filename to classify as %q, got %q", "MacPS", got)
	}
	if got := classifyMacFamily(tokens, "Generic_GUC_PrinterSoftware_11202025.dmg"); got != "" {
		t.Errorf("expected the zero-PPD Lexmark decoy filename to never classify into any Sharp family, got %q", got)
	}
}

// TestStripLanguageSuffix_SharpStripsGenericPPDSuffix locks in the real
// finding that every one of Sharp's own 147 real PPDs (confirmed live,
// 2026-09-13, via the real MX-C55c_2512a_MacPS.dmg payload) carries a
// generic, non-language "*NickName" suffix of " PPD" - not a distinguishing
// driver family the way Canon's own PS/PPD/UFRII suffixes are, since Sharp
// ships only one real driver. "PPD" is listed second in Sharp's own
// macFamilyPreference entry purely so this strips cleanly (the same trick
// Canon's own real "PPD" family already relies on), leaving a friendly model
// name with no redundant "PPD" in it.
func TestStripLanguageSuffix_SharpStripsGenericPPDSuffix(t *testing.T) {
	tokens := macFamilyPreference["Sharp"]
	model, matched := stripLanguageSuffix(`SHARP MX-3071S PPD`, tokens)
	if model != "SHARP MX-3071S" {
		t.Errorf("expected the generic PPD suffix stripped, got model=%q", model)
	}
	if matched != "PPD" {
		t.Errorf("expected the PPD token to be reported as the match, got %q", matched)
	}
	if got := languageDisplayName("MacPS"); got != "Driver" {
		t.Errorf(`expected languageDisplayName("MacPS") == "Driver", got %q`, got)
	}
}

// TestClassifyMacFamily_XeroxTokenMatchesRealDriverFilenames locks in the
// real finding (2026-09-13) that, like Kyocera, Xerox's own real driver
// filename always contains the manufacturer's own name
// ("XeroxDrivers_5.19.3_2562.dmg" and 7 other real versioned downloads
// across every real OS-version folder inspected) - a single "Xerox" token
// trivially classifies every one of them, unlocking the catalog-driven model
// index rather than the guess-based fallback.
func TestClassifyMacFamily_XeroxTokenMatchesRealDriverFilenames(t *testing.T) {
	tokens := macFamilyPreference["Xerox"]
	for _, name := range []string{
		"XeroxDrivers_5.6.0_2187.dmg",
		"XeroxDrivers_5.19.3_2562.dmg",
	} {
		if got := classifyMacFamily(tokens, name); got != "Xerox" {
			t.Errorf("expected %q to classify as %q, got %q", name, "Xerox", got)
		}
	}
	if got := languageDisplayName("Xerox"); got != "Driver" {
		t.Errorf(`expected languageDisplayName("Xerox") == "Driver", got %q`, got)
	}
}

// TestMacSubPackagePPDFallback_RicohAndXeroxShareTheSameFallback guards that
// the extraction fallback generalized (2026-09-13) from Ricoh-only
// (RicohPPDPathForDefaults/ricohPPDExtractionFallback) into
// pathFragmentPPDExtractionFallback actually covers Xerox too - both real
// manufacturers whose own real PPDs carry no ".ppd" anywhere in their own
// filename (a bare ".gz" for both, confirmed live independently for each),
// so both need the same content-based, path-fragment fallback the fast
// extension-based cpio glob alone can never satisfy.
func TestMacSubPackagePPDFallback_RicohAndXeroxShareTheSameFallback(t *testing.T) {
	if macSubPackagePPDFallback("Ricoh") == nil {
		t.Error("expected Ricoh to have a non-nil PPD extraction fallback")
	}
	if macSubPackagePPDFallback("Xerox") == nil {
		t.Error("expected Xerox to have a non-nil PPD extraction fallback")
	}
	if macSubPackagePPDFallback("Toshiba") == nil {
		t.Error("expected Toshiba to have a non-nil PPD extraction fallback")
	}
	if macSubPackagePPDFallback("Canon") != nil {
		t.Error("expected Canon to have no PPD extraction fallback - its real PPDs are found by the fast extension-based glob")
	}
}

// TestClassifyMacFamily_ToshibaTokenMatchesRealDriverFilename locks in the
// real finding (2026-09-13) that, like Kyocera/Xerox, Toshiba's own real
// driver filename ("TOSHIBA_ColorMFP.dmg.gz") always contains the
// manufacturer's own name - a single "Toshiba" token trivially classifies
// it, unlocking the catalog-driven model index the same way.
func TestClassifyMacFamily_ToshibaTokenMatchesRealDriverFilename(t *testing.T) {
	tokens := macFamilyPreference["Toshiba"]
	if got := classifyMacFamily(tokens, "TOSHIBA_ColorMFP.dmg.gz"); got != "Toshiba" {
		t.Errorf("expected %q to classify as %q, got %q", "TOSHIBA_ColorMFP.dmg.gz", "Toshiba", got)
	}
	if got := languageDisplayName("Toshiba"); got != "Driver" {
		t.Errorf(`expected languageDisplayName("Toshiba") == "Driver", got %q`, got)
	}
}
