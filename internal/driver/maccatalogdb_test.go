package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMacCatalogFileName(t *testing.T) {
	tests := []struct{ mfg, want string }{
		{"Canon", "catalog.canon.json"},
		{"Konica Minolta", "catalog.konicaminolta.json"},
	}
	for _, tt := range tests {
		if got := MacCatalogFileName(tt.mfg); got != tt.want {
			t.Errorf("MacCatalogFileName(%q) = %q, want %q", tt.mfg, got, tt.want)
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
	path := filepath.Join(t.TempDir(), "Canon", MacCatalogFileName("Canon"))
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
	pkg := MacPackage{Path: "/Drivers/UFRII_v10.19.25_mac.dmg", ModTime: now, Size: 1000}
	cat := MacManufacturerCatalog{
		Provenance: map[string]MacFamilyProvenance{
			"UFRII": {Chain: []MacPackageRef{{Path: pkg.Path, ModTime: pkg.ModTime, Size: pkg.Size}}},
		},
		Models: map[string][]MacCatalogVariant{},
	}

	if !cat.IsCurrent("UFRII", pkg) {
		t.Error("expected IsCurrent to match an identical path/modtime/size")
	}
	if cat.IsCurrent("PS", pkg) {
		t.Error("expected no match for a family with no recorded provenance at all")
	}
	if cat.IsCurrent("UFRII", MacPackage{Path: pkg.Path, ModTime: now, Size: 999}) {
		t.Error("expected no match once Size differs (a newer download replaced the file)")
	}
	if cat.IsCurrent("UFRII", MacPackage{Path: pkg.Path, ModTime: now.Add(time.Hour), Size: pkg.Size}) {
		t.Error("expected no match once ModTime differs")
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
