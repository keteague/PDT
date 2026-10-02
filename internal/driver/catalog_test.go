package driver

import (
	"os"
	"path/filepath"
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

// TestBuildCatalog_RecoversStaleMarkerArchivePath is the real-world bug's
// own end-to-end regression test (2026-10-02, GitHub issue: stale
// .pdt-source markers - confirmed live against R.K. Black Inc's own Drivers
// folder). The marker (written once, long ago, by whatever machine first
// extracted this .inf) points at a drive letter that doesn't exist on this
// machine at all; the real archive still sits right where it always has,
// relative to the Drivers folder. BuildCatalog must resolve ArchEntry's own
// ArchivePath to the real, existing file - see
// recoverArchivePathFromMarkerLocation's own doc comment for the mechanism -
// not the stale marker string verbatim, which would fail Deploy-time
// extraction with "the system cannot find the path specified" even though
// the driver package is sitting right there.
func TestBuildCatalog_RecoversStaleMarkerArchivePath(t *testing.T) {
	root := t.TempDir()
	mfgPath := filepath.Join(root, "Canon")
	if err := os.MkdirAll(mfgPath, 0o755); err != nil {
		t.Fatal(err)
	}
	realArchive := filepath.Join(mfgPath, "Foo.zip")
	writeTestZip(t, realArchive, "driver.inf", testZipInf)

	cacheDir := filepath.Join(mfgPath, PdtInfCacheDirName, "Foo")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "driver.inf"), []byte(testZipInf), 0o644); err != nil {
		t.Fatal(err)
	}
	// A stale marker from an older PDT version (before writeSourceMarker
	// stored a root-relative path), left over from a different machine/
	// drive letter - the exact real-world shape confirmed live, missing
	// separator and all (see resolveAgainstExe's own doc comment, app.go).
	// Written directly rather than via writeSourceMarker, which now always
	// stores relative - this simulates what's still actually sitting on a
	// real, long-lived Drivers folder today.
	if err := os.WriteFile(filepath.Join(cacheDir, pdtSourceMarkerName), []byte(`E:Drivers\Windows\11\Canon\Foo.zip`), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, err := BuildCatalog(root)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	entry, ok := findArchEntry(cat, "Canon", "Canon Zipped Test Driver")
	if !ok {
		t.Fatalf("expected Canon Zipped Test Driver to be cataloged, got %v", cat["Canon"])
	}
	if entry.ArchivePath != realArchive {
		t.Errorf("ArchEntry.ArchivePath = %q, want the real, existing archive %q (recovered from the marker's own live location, not its stale stored string)", entry.ArchivePath, realArchive)
	}
}

// findArchEntry digs out the one ArchEntry a test cares about from
// Catalog's own nested map-of-maps-of-maps shape, regardless of version/arch
// key - a thin helper so a test doesn't need to know those keys in advance.
func findArchEntry(cat Catalog, mfg, driverName string) (ArchEntry, bool) {
	for _, archMap := range cat[mfg][driverName] {
		for _, entry := range archMap {
			return entry, true
		}
	}
	return ArchEntry{}, false
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

// TestPreferredArchTokens_FixedUniversalOrder guards against PreferredArchTokens
// ever going back to switching on runtime.GOARCH - the host machine running
// PDT (including a macOS technician's own arm64/amd64 Mac) has nothing to do
// with the architecture of the remote Windows machine a driver is actually
// being deployed to (see PreferredArchTokens' own doc comment).
func TestPreferredArchTokens_FixedUniversalOrder(t *testing.T) {
	toks := PreferredArchTokens()
	want := []string{"x64", "64bit", "arm64", "32bit"}
	if len(toks) != len(want) {
		t.Fatalf("PreferredArchTokens() = %v, want %v", toks, want)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Errorf("PreferredArchTokens()[%d] = %q, want %q (full: %v)", i, toks[i], want[i], toks)
		}
	}
}
