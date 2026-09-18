package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRelToDriversRoot_StoresForwardSlashRegardlessOfOSSeparator guards a
// real bug caught live before shipping GitHub issue #13's own fix: on
// Windows, filepath.Rel returns a backslash-separated relative path, and
// storing that verbatim would make the catalog file unreadable on a real
// Mac later - filepath.Join there treats a literal backslash as just
// another filename character, not a path separator, so it would never
// resolve to the real nested file. relToDriversRoot must always normalize
// to forward slash on the way out, and absFromDriversRoot must convert back
// via FromSlash on the way in, regardless of which OS is running right now.
func TestRelToDriversRoot_StoresForwardSlashRegardlessOfOSSeparator(t *testing.T) {
	driversRoot := filepath.Join("C:", "Users", "Ken", "Drivers")
	absPath := filepath.Join(driversRoot, "macOS", "Kyocera", "14-Sonoma", "Kyocera Web build 2026.07.03.dmg")

	rel := relToDriversRoot(driversRoot, absPath)
	want := "macOS/Kyocera/14-Sonoma/Kyocera Web build 2026.07.03.dmg"
	if rel != want {
		t.Errorf("relToDriversRoot returned %q, want forward-slash-normalized %q", rel, want)
	}

	got := absFromDriversRoot(driversRoot, rel)
	if got != absPath {
		t.Errorf("absFromDriversRoot(%q, %q) = %q, want %q", driversRoot, rel, got, absPath)
	}
}

func TestCatalogFileName(t *testing.T) {
	tests := []struct{ mfg, want string }{
		{"Canon", "catalog.canon.json"},
		{"Konica Minolta", "catalog.konicaminolta.json"},
	}
	for _, tt := range tests {
		if got := CatalogFileName(tt.mfg); got != tt.want {
			t.Errorf("CatalogFileName(%q) = %q, want %q", tt.mfg, got, tt.want)
		}
	}
}

func TestLoadMacManufacturerCatalog_MissingFileReturnsEmptyReadyToUse(t *testing.T) {
	cat := LoadMacManufacturerCatalog(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if cat.Provenance == nil || cat.Models == nil {
		t.Fatalf("expected non-nil maps even for a missing file, got %+v", cat)
	}
	if len(cat.Provenance) != 0 || len(cat.Models) != 0 {
		t.Errorf("expected an empty catalog, got %+v", cat)
	}
}

func TestLoadMacManufacturerCatalog_CorruptFileReturnsEmptyReadyToUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.canon.json")
	if err := SaveMacManufacturerCatalog(path, MacManufacturerCatalog{Provenance: map[string]MacFamilyProvenance{}, Models: map[string][]MacCatalogVariant{}}); err != nil {
		t.Fatalf("SaveMacManufacturerCatalog: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("corrupting the file: %v", err)
	}
	cat := LoadMacManufacturerCatalog(path)
	if cat.Provenance == nil || cat.Models == nil {
		t.Fatalf("expected non-nil maps even for a corrupt file, got %+v", cat)
	}
}

func TestSaveLoadMacManufacturerCatalog_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Canon", CatalogFileName("Canon"))
	now := time.Now().Truncate(time.Second) // JSON round-trips to second precision
	original := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{
			"UFRII": {
				Chain: []MacPackageRef{
					{Path: "/Drivers/macOS/Canon/26-Tahoe/UFRII_v10.19.25_mac.dmg", ModTime: now, Size: 12345},
					{Path: "mac-UFRII-LIPSLX-v101925-05.dmg"},
					{Path: "UFRII_LT_LIPS_LX_Installer.pkg"},
					{Path: "Canon_Family_Printer_Device.pkg", Version: "10.19.25"},
				},
				IndexedAt: now,
			},
		},
		Models: map[string][]MacCatalogVariant{
			"Canon iR-ADV C5840/5850": {
				{Language: "UFRII", NickName: "Canon iR-ADV C5840/5850", Filename: "CNPZUIRAC5840ZU.ppd.gz", PackagePath: "/Drivers/.../UFRII_v10.19.25_mac.dmg"},
			},
		},
	}
	if err := SaveMacManufacturerCatalog(path, original); err != nil {
		t.Fatalf("SaveMacManufacturerCatalog: %v", err)
	}
	loaded := LoadMacManufacturerCatalog(path)

	prov := loaded.Provenance["UFRII"]
	if len(prov.Chain) != 4 {
		t.Fatalf("expected a 4-entry provenance chain to round-trip, got %d: %+v", len(prov.Chain), prov.Chain)
	}
	if prov.Chain[0].Size != 12345 || !prov.Chain[0].ModTime.Equal(now) {
		t.Errorf("outer chain entry didn't round-trip identity: %+v", prov.Chain[0])
	}
	if prov.Chain[3].Version != "10.19.25" {
		t.Errorf("sub-package version didn't round-trip: %+v", prov.Chain[3])
	}
	if len(loaded.Models["Canon iR-ADV C5840/5850"]) != 1 {
		t.Errorf("expected the one model entry to round-trip, got %+v", loaded.Models)
	}
}

func TestMacManufacturerCatalog_IsCurrent(t *testing.T) {
	now := time.Now()
	driversRoot := "/Drivers"
	pkg := MacPackage{Path: "/Drivers/macOS/Canon/UFRII_v10.19.25_mac.dmg", ModTime: now, Size: 1000}
	cat := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{
			// GitHub issue #13: stored driversRoot-relative, matching what
			// BuildMacModelIndex itself now writes - not pkg.Path verbatim.
			"UFRII": {Chain: []MacPackageRef{{Path: relToDriversRoot(driversRoot, pkg.Path), ModTime: pkg.ModTime, Size: pkg.Size}}},
		},
		Models: map[string][]MacCatalogVariant{},
	}

	if !cat.IsCurrent("UFRII", pkg, driversRoot) {
		t.Error("expected IsCurrent to match an identical path/modtime/size")
	}
	if cat.IsCurrent("PS", pkg, driversRoot) {
		t.Error("expected no match for a family with no recorded provenance at all")
	}
	if cat.IsCurrent("UFRII", MacPackage{Path: pkg.Path, ModTime: now, Size: 999}, driversRoot) {
		t.Error("expected no match once Size differs (a newer download replaced the file)")
	}
	if cat.IsCurrent("UFRII", MacPackage{Path: pkg.Path, ModTime: now.Add(time.Hour), Size: pkg.Size}, driversRoot) {
		t.Error("expected no match once ModTime differs")
	}
}

// TestMacManufacturerCatalog_IsCurrent_DifferentDriversRoot is the real
// scenario GitHub issue #13 fixes: the identical package, indexed once with
// one driversRoot (e.g. a Windows machine's own local path) then checked
// again with a *different* driversRoot (e.g. a real Mac's own path for the
// same synced Drivers folder) - relToDriversRoot's own relative form is the
// same in both cases, so IsCurrent correctly still matches, unlike the old
// absolute-path comparison which never could.
func TestMacManufacturerCatalog_IsCurrent_DifferentDriversRoot(t *testing.T) {
	now := time.Now()
	// The stored form a real Windows-run PDT actually writes - confirmed
	// live during GitHub issue #13's own verification (a real
	// catalog.kyocera.json this produced on Windows) to always be
	// forward-slash-normalized, driversRoot-relative, with no drive letter.
	// Hardcoded here rather than computed via relToDriversRoot("C:\...",
	// ...) inside this test itself - confirmed live as a real bug (caught
	// by real macOS CI, not guessed): that call runs using *whichever OS is
	// executing this test*, and on a real Mac, filepath.Rel/ToSlash don't
	// treat a Windows-style backslash string as having any directory
	// structure at all (no forward slashes present) - it produced a
	// nonsense "relative path" that could never match what IsCurrent then
	// computes for a real Mac path, failing the very scenario this test
	// means to prove works. The production code itself was never wrong -
	// only this test's own attempt to simulate "what Windows would have
	// written" from inside whichever process happens to run it.
	storedPath := "macOS/Canon/UFRII_v10.19.25_mac.dmg"
	cat := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{
			"UFRII": {Chain: []MacPackageRef{{Path: storedPath, ModTime: now, Size: 1000}}},
		},
		Models: map[string][]MacCatalogVariant{},
	}

	macRoot := "/Users/tech/Library/Application Support/PDT/Drivers"
	macPkg := MacPackage{Path: macRoot + "/macOS/Canon/UFRII_v10.19.25_mac.dmg", ModTime: now, Size: 1000}
	if !cat.IsCurrent("UFRII", macPkg, macRoot) {
		t.Error("expected a catalog built under a Windows driversRoot to still be recognized as current once read under a real Mac's own different driversRoot")
	}
}

// TestMacManufacturerCatalog_IsCurrent_LegacyAbsolutePaths confirms the
// backward-compatibility contract GitHub issue #13's own plan committed to:
// a catalog.<mfg>.json written before this fix (Chain[0].Path stored
// absolute, not relative) is never rejected or misparsed - it just never
// matches (the exact same "always stale, re-index once" behavior it already
// had before this fix existed), never a crash or a wrong/garbled path.
func TestMacManufacturerCatalog_IsCurrent_LegacyAbsolutePaths(t *testing.T) {
	now := time.Now()
	driversRoot := "/Drivers"
	legacyAbsPath := "/Drivers/macOS/Canon/UFRII_v10.19.25_mac.dmg"
	cat := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{
			"UFRII": {Chain: []MacPackageRef{{Path: legacyAbsPath, ModTime: now, Size: 1000}}},
		},
		Models: map[string][]MacCatalogVariant{},
	}

	pkg := MacPackage{Path: legacyAbsPath, ModTime: now, Size: 1000}
	if cat.IsCurrent("UFRII", pkg, driversRoot) {
		t.Error("expected a legacy absolute-path entry to be treated as stale (not matched), not silently trusted or errored on")
	}
}

func TestDiffModels_AddedAndRemoved(t *testing.T) {
	cat := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{},
		Models: map[string][]MacCatalogVariant{
			"Model A": {{Language: "UFRII"}},
			"Model B": {{Language: "UFRII"}},
			"Model C": {{Language: "PS"}}, // different family - irrelevant to a UFRII diff
		},
	}
	current := map[string][]MacCatalogVariant{
		"Model B": {{Language: "UFRII"}},
		"Model D": {{Language: "UFRII"}},
	}

	added, removed := DiffModels(cat, "UFRII", current)
	if len(added) != 1 || added[0] != "Model D" {
		t.Errorf("added = %v, want [Model D]", added)
	}
	if len(removed) != 1 || removed[0] != "Model A" {
		t.Errorf("removed = %v, want [Model A]", removed)
	}
}

func TestFormatModelDiff_TruncatesLongLists(t *testing.T) {
	got := formatModelDiff([]string{"A", "B", "C", "D", "E", "F", "G"}, nil)
	want := "+7 new: A, B, C, D, E (+2 more)"
	if got != want {
		t.Errorf("formatModelDiff = %q, want %q", got, want)
	}
}
