package driver

import (
	"runtime"
	"testing"
	"time"
)

func TestCandidates_MultiVersionDecoratesBothWithArchNote(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("this table assumes the dev/CI host is amd64")
	}
	cat := testCatalog(t)
	modelIndex := BuildModelIndex(cat)
	got := Candidates(cat, modelIndex, "Kyocera", "FS-1100", "")

	want := []string{
		"Kyocera FS-1100 KX (v8.7.0422.0 - 2026-04-22, 64bit)",
		"Kyocera FS-1100 KX (v8.6.1022.0 - 2025-10-22, 64bit)",
	}
	if len(got) != len(want) {
		t.Fatalf("Candidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Candidates()[%d] = %q, want %q (newest-first order)", i, got[i], want[i])
		}
	}
}

func TestCandidates_SingleCompatibleVersionStaysPlain(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("this table assumes the dev/CI host is amd64")
	}
	cat := testCatalog(t)
	modelIndex := BuildModelIndex(cat)

	got := Candidates(cat, modelIndex, "Canon", "", "")
	found := false
	for _, c := range got {
		if c == "Canon Generic Plus UFR II V350" {
			t.Errorf("etc/ alias name leaked into candidates: %v", got)
		}
		if c == "Canon Generic Plus UFR II" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected plain 'Canon Generic Plus UFR II' label (only 1 version group, no decoration), got %v", got)
	}

	got = Candidates(cat, modelIndex, "HP", "", "")
	found = false
	for _, c := range got {
		if c == "HP Universal Printing PCL 6" {
			found = true
		}
		if c != "HP Universal Printing PCL 6" {
			t.Errorf("expected the incompatible arm64-only HP version group to be filtered out on amd64, got extra candidate %q", c)
		}
	}
	if !found {
		t.Errorf("expected plain 'HP Universal Printing PCL 6' label (arm64-only version filtered out), got %v", got)
	}
}

// TestCandidateDetails_CarriesSourcePackagePerVersion: three versions of the
// same driver name each report their own package (archive when known, else
// the .inf) - the Windows Driver dropdown's tooltip data.
func TestCandidateDetails_CarriesSourcePackagePerVersion(t *testing.T) {
	d1 := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2025, 9, 8, 0, 0, 0, 0, time.UTC)
	catalog := Catalog{
		"Konica Minolta": {
			"KONICA MINOLTA Universal PCL": {
				"v1|2025-09-01": {"x64": {InfPath: "cache/a.inf", ArchivePath: "KM/Universal_v1.zip", Date: d1, Version: "3.9.1310.0"}},
				"v2|2025-09-08": {"x64": {InfPath: "KM/extracted/b.inf", Date: d2, Version: "3.9.1203.500"}},
			},
		},
	}
	got := CandidateDetails(catalog, nil, "Konica Minolta", "", "")
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %+v", got)
	}
	if got[0].Sources[0] != "KM/extracted/b.inf" {
		t.Errorf("newest version should list its .inf (no archive), got %+v", got[0])
	}
	if got[1].Sources[0] != "KM/Universal_v1.zip" {
		t.Errorf("older version should list its archive, got %+v", got[1])
	}
	if plain := Candidates(catalog, nil, "Konica Minolta", "", ""); len(plain) != 2 || plain[0] != got[0].Label {
		t.Errorf("Candidates and CandidateDetails must agree on labels/order, got %v vs %+v", plain, got)
	}
}
