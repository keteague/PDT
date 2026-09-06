package driver

import (
	"runtime"
	"testing"
)

func testCatalog(t *testing.T) Catalog {
	t.Helper()
	cat, err := BuildCatalog("testdata")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	return cat
}

func TestBuildCatalog_CanonMergesArchesUnderOneVersion(t *testing.T) {
	cat := testCatalog(t)
	versions := cat["Canon"]["Canon Generic Plus UFR II"]
	if len(versions) != 1 {
		t.Fatalf("expected exactly 1 version group for Canon Generic Plus UFR II, got %d: %v", len(versions), versions)
	}
	for vkey, archMap := range versions {
		if len(archMap) != 2 {
			t.Fatalf("expected 2 arches (32bit, x64) in version group %q, got %d: %v", vkey, len(archMap), archMap)
		}
		if _, ok := archMap["32bit"]; !ok {
			t.Errorf("expected 32bit arch entry (normalized from Canon's '32BIT' folder), got %v", archMap)
		}
		if _, ok := archMap["x64"]; !ok {
			t.Errorf("expected x64 arch entry, got %v", archMap)
		}
	}
}

func TestBuildCatalog_SkipsEtcDuplicates(t *testing.T) {
	cat := testCatalog(t)
	if _, ok := cat["Canon"]["Canon Generic Plus UFR II V350"]; ok {
		t.Error("etc/ folder's version-suffixed duplicate name should have been skipped, but was found in the catalog")
	}
}

func TestBuildCatalog_SkipsArchiveFolder(t *testing.T) {
	cat := testCatalog(t)
	versions := cat["Kyocera"]["Kyocera FS-1100 KX"]
	if len(versions) != 2 {
		t.Fatalf("expected exactly 2 version groups for Kyocera FS-1100 KX (8.7A.0422 + 8.6.1022, Archive copy excluded), got %d: %v", len(versions), versions)
	}
}

func TestBuildCatalog_HPArchBakedIntoFolderName(t *testing.T) {
	cat := testCatalog(t)
	versions := cat["HP"]["HP Universal Printing PCL 6"]
	if len(versions) != 2 {
		t.Fatalf("expected 2 separate version groups for HP (x64 and arm64 builds have distinct DriverVer), got %d: %v", len(versions), versions)
	}
	sawX64, sawArm64 := false, false
	for _, archMap := range versions {
		if _, ok := archMap["x64"]; ok {
			sawX64 = true
		}
		if _, ok := archMap["arm64"]; ok {
			sawArm64 = true
		}
	}
	if !sawX64 || !sawArm64 {
		t.Errorf("expected one version group with x64 (from 'upd-pcl6-win11-x64-...' folder) and one with arm64 (from '...-ARM64-...'), got %v", versions)
	}
}

func TestBuildCatalog_KyoceraVersionsAndDates(t *testing.T) {
	cat := testCatalog(t)
	versions := cat["Kyocera"]["Kyocera FS-1100 KX"]
	found871, found861 := false, false
	for _, archMap := range versions {
		e, ok := archMap["64bit"]
		if !ok {
			t.Errorf("expected 64bit arch entry, got %v", archMap)
			continue
		}
		switch e.Version {
		case "8.7.0422.0":
			found871 = true
			if got := e.Date.Format("2006-01-02"); got != "2026-04-22" {
				t.Errorf("8.7.0422.0 date = %s, want 2026-04-22", got)
			}
		case "8.6.1022.0":
			found861 = true
			if got := e.Date.Format("2006-01-02"); got != "2025-10-22" {
				t.Errorf("8.6.1022.0 date = %s, want 2025-10-22", got)
			}
		}
	}
	if !found871 || !found861 {
		t.Errorf("expected both 8.7.0422.0 and 8.6.1022.0 present, got %v", versions)
	}
}

func TestBuildCatalog_WindowsVersionNestedLayout(t *testing.T) {
	// Mirrors the real on-disk layout as reorganized: driversRoot/Windows/
	// <any version folder>/<Manufacturer>/... rather than driversRoot/
	// <Manufacturer>/... directly - testdata_windows_layout/Windows/11/Canon
	// is a copy of testdata/Canon one level deeper, under a "Windows/11"
	// prefix, to prove this layout is actually found and scanned.
	cat, err := BuildCatalog("testdata_windows_layout")
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	versions := cat["Canon"]["Canon Generic Plus UFR II"]
	if len(versions) != 1 {
		t.Fatalf("expected exactly 1 version group for Canon Generic Plus UFR II under the nested Windows/11 layout, got %d: %v", len(versions), versions)
	}
	for _, archMap := range versions {
		if _, ok := archMap["32bit"]; !ok {
			t.Errorf("expected 32bit arch entry, got %v", archMap)
		}
		if _, ok := archMap["x64"]; !ok {
			t.Errorf("expected x64 arch entry, got %v", archMap)
		}
	}
}

func TestPreferredArchTokens_HostArch(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("this table assumes the dev/CI host is amd64")
	}
	toks := PreferredArchTokens()
	if len(toks) != 2 || toks[0] != "x64" || toks[1] != "64bit" {
		t.Errorf("PreferredArchTokens() on amd64 = %v, want [x64 64bit]", toks)
	}
}
