package driver

import (
	"testing"
	"time"
)

// PreferredArchTokens is a fixed, universal order now (arch.go) - not
// switched on runtime.GOARCH anymore - so every test in this file runs the
// same way regardless of which host actually runs it.
func TestCandidates_MultiVersionDecoratesBothWithArchNote(t *testing.T) {
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
}

// TestCandidates_CrossArchVersionsBothOfferedRegardlessOfHost guards
// PreferredArchTokens' own fixed, universal order (arch.go): an x64-only
// version and an arm64-only version of the same driver name (HP's real
// testdata - different DriverVer/date per architecture, same shape as HP's
// real Universal Print Driver releases) must both show up as candidates on
// every host PDT itself runs on, x64-labeled one first - never filtered
// down to whichever one happens to match the host machine's own CPU
// architecture (the real, reported bug this fixed: on an Apple Silicon Mac,
// GOARCH-based filtering used to hide every non-arm64-only driver
// entirely, including manufacturers with no real ARM64 Windows build at
// all - Canon, Sharp, Toshiba, Xerox, confirmed live 2026-09-21).
func TestCandidates_CrossArchVersionsBothOfferedRegardlessOfHost(t *testing.T) {
	cat := testCatalog(t)
	modelIndex := BuildModelIndex(cat)

	got := Candidates(cat, modelIndex, "HP", "", "")
	want := []string{
		"HP Universal Printing PCL 6 (v61.360.01.26819 - 2026-05-20, x64)",
		"HP Universal Printing PCL 6 (v61.360.01.26778 - 2026-04-23, arm64)",
	}
	if len(got) != len(want) {
		t.Fatalf("Candidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Candidates()[%d] = %q, want %q (x64 preferred/newest-first order)", i, got[i], want[i])
		}
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
				"v1|2025-09-01": {"any": {InfPath: "cache/a.inf", ArchivePath: "KM/Universal_v1.zip", Date: d1, Version: "3.9.1310.0"}},
				"v2|2025-09-08": {"any": {InfPath: "KM/extracted/b.inf", Date: d2, Version: "3.9.1203.500"}},
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

// TestCandidateDetails_ExcludesFaxDrivers guards the real, reported bug: a
// Konica Minolta Universal Driver .inf declares a real "...Universal FAX"
// entry alongside its real print drivers - PDT only ever deploys print
// queues, so a fax "driver" is never a usable Windows Driver candidate.
func TestCandidateDetails_ExcludesFaxDrivers(t *testing.T) {
	catalog := Catalog{
		"Konica Minolta": {
			"KONICA MINOLTA Universal PCL": {"v1|2025-09-01": {"any": {InfPath: "a.inf"}}},
			"KONICA MINOLTA Universal FAX": {"v1|2025-09-01": {"any": {InfPath: "b.inf"}}},
		},
	}
	got := Candidates(catalog, nil, "Konica Minolta", "", "")
	if len(got) != 1 || got[0] != "KONICA MINOLTA Universal PCL" {
		t.Errorf("expected only the PCL driver, FAX excluded, got %v", got)
	}
}

// TestCandidateDetails_PrefersVersionedNameOverBareDuplicate guards the
// other real, reported Konica Minolta bug: the identical .inf declares the
// exact same driver under both a bare name and a " vX.Y" version-suffixed
// name (bareNameDuplicates) - only the versioned one should ever reach the
// dropdown ("Only show the one that has a driver version number in it").
// The bare name stays selectable when no version-suffixed sibling shares its
// own InfPath, so an unrelated real driver that merely happens to share a
// name prefix is never suppressed by mistake.
func TestCandidateDetails_PrefersVersionedNameOverBareDuplicate(t *testing.T) {
	catalog := Catalog{
		"Konica Minolta": {
			"KONICA MINOLTA Universal PCL":         {"v1|2025-09-01": {"any": {InfPath: "shared.inf"}}},
			"KONICA MINOLTA Universal PCL v3.9.13": {"v1|2025-09-01": {"any": {InfPath: "shared.inf"}}},
			"KONICA MINOLTA Universal PS":          {"v1|2025-09-01": {"any": {InfPath: "unrelated.inf"}}},
		},
	}
	got := Candidates(catalog, nil, "Konica Minolta", "", "")
	want := map[string]bool{"KONICA MINOLTA Universal PCL v3.9.13": true, "KONICA MINOLTA Universal PS": true}
	if len(got) != len(want) {
		t.Fatalf("Candidates() = %v, want exactly %v", got, want)
	}
	for _, c := range got {
		if !want[c] {
			t.Errorf("unexpected candidate %q (bare duplicate should have been suppressed): %v", c, got)
		}
	}
}
