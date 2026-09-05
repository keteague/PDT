package driver

import (
	"runtime"
	"testing"
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
