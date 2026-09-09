package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testMacCatalog builds the catalog from testdata_mac, first forcing
// deterministic, distinct mtimes on the two Canon fixtures that
// TestResolveMac_PicksNewestByModTime depends on. git does not preserve
// mtimes across a clone/checkout - both files land with essentially the same
// checkout-time mtime - so without this, ResolveMac's "pick the newest by
// mtime" comparison degenerates into a tie that resolves in scan order
// rather than by the fixtures' intended Older/Newer semantics. Confirmed
// live: this made the test flaky on a fresh checkout.
func testMacCatalog(t *testing.T) MacCatalog {
	t.Helper()
	now := time.Now()
	older := filepath.Join("testdata_mac", "macOS", "Canon", "15", "CanonDriverOlder.dmg")
	newer := filepath.Join("testdata_mac", "macOS", "Canon", "26", "CanonDriverNewer.pkg")
	if err := os.Chtimes(older, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("os.Chtimes(%s): %v", older, err)
	}
	if err := os.Chtimes(newer, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("os.Chtimes(%s): %v", newer, err)
	}
	cat, err := BuildMacCatalog("testdata_mac")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	return cat
}

func TestBuildMacCatalog_FindsPackagesUnderVersionFolders(t *testing.T) {
	cat := testMacCatalog(t)
	pkgs := cat.Packages["Canon"]
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 Canon packages (15/ + 26/, Archive excluded), got %d: %v", len(pkgs), pkgs)
	}
}

func TestBuildMacCatalog_SkipsArchiveFolder(t *testing.T) {
	cat := testMacCatalog(t)
	for _, p := range cat.Packages["Canon"] {
		if p.Path == "" {
			continue
		}
		if containsSegment(p.Path, "Archive") {
			t.Errorf("expected Archive folder to be skipped, but found %s in catalog", p.Path)
		}
	}
}

func TestBuildMacCatalog_KindDetectedFromExtension(t *testing.T) {
	cat := testMacCatalog(t)
	sawPkg, sawDmg := false, false
	for _, p := range cat.Packages["Canon"] {
		switch p.Kind {
		case MacPackagePkg:
			sawPkg = true
		case MacPackageDmg:
			sawDmg = true
		}
	}
	if !sawPkg || !sawDmg {
		t.Errorf("expected both a .pkg and a .dmg entry for Canon, got %v", cat.Packages["Canon"])
	}
}

func TestBuildMacCatalog_OpenPrintingPPDsFlatBucket(t *testing.T) {
	cat := testMacCatalog(t)
	ppds := cat.OpenPrintingPPDs["Ricoh"]
	if len(ppds) != 2 {
		t.Fatalf("expected 2 Ricoh OpenPrinting PPDs (.ppd + .ppd.gz), got %d: %v", len(ppds), ppds)
	}
}

func TestBuildMacCatalog_ManufacturerFolderNameFoldsSpaces(t *testing.T) {
	// "Konica Minolta" (this app's display name) folds onto a "KonicaMinolta"
	// (no space) folder the same way BuildCatalog's Windows side already
	// requires - covered here as a regression guard even though there's no
	// dedicated fixture folder for it, by asserting the fold-match helper
	// itself (already exercised for Windows) applies unchanged.
	if !foldMatchIgnoringSpaces("Konica Minolta", "KonicaMinolta") {
		t.Error("expected foldMatchIgnoringSpaces to match \"Konica Minolta\" against \"KonicaMinolta\"")
	}
}

func TestBuildMacCatalog_MissingMacOSFolderIsEmptyNotError(t *testing.T) {
	cat, err := BuildMacCatalog("testdata")
	if err != nil {
		t.Fatalf("BuildMacCatalog: %v", err)
	}
	if len(MacManufacturersWithPackages(cat)) != 0 {
		t.Errorf("expected no manufacturers with packages against a Windows-only testdata root, got %v", MacManufacturersWithPackages(cat))
	}
}

func TestMacManufacturersWithPackages(t *testing.T) {
	cat := testMacCatalog(t)
	got := MacManufacturersWithPackages(cat)
	wantCanon, wantKyocera, wantRicoh := false, false, false
	for _, m := range got {
		switch m {
		case "Canon":
			wantCanon = true
		case "Kyocera":
			wantKyocera = true
		case "Ricoh":
			wantRicoh = true
		}
	}
	if !wantCanon || !wantKyocera {
		t.Errorf("expected Canon and Kyocera (installer packages present), got %v", got)
	}
	if !wantRicoh {
		t.Errorf("expected Ricoh (OpenPrinting-only PPDs present), got %v", got)
	}
}

func containsSegment(path, segment string) bool {
	for _, s := range strings.Split(filepath.ToSlash(path), "/") {
		if s == segment {
			return true
		}
	}
	return false
}
