package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestPPD(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestBuildOpenPrintingNickNames_MatchesByRealNickNameNotFilename is the
// direct regression test for the real gap Ken hit live (GitHub issue #16
// follow-up, 2026-09-19): a real OpenPrinting PPD's own filename is
// routinely cryptic ("CNPZUIRAC5045ZU.ppd" for "Canon iR-ADV C5045/5051")
// and shares no matchable text with the friendly model name a technician
// actually picks - see ppdNickNameRe's own doc comment - so before its real
// *NickName content is cached, narrowing by Model can't tell two Canon PPDs
// apart at all and correctly finds nothing (Ken's own later, explicit
// revision, 2026-09-19: "if there is no OpenPrinting driver candidate, we
// should not show the complete list" - a technician still isn't left with
// nothing to pick at all, since macDriverCandidatesWithSource always offers
// Apple's own Generic PostScript driver alongside whatever this returns);
// after caching, it correctly narrows to just the one whose real NickName
// matches.
func TestBuildOpenPrintingNickNames_MatchesByRealNickNameNotFilename(t *testing.T) {
	dir := t.TempDir()
	c5045 := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC5045ZU.ppd")
	c3226 := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC3226ZU.ppd")
	writeTestPPD(t, c5045, "*NickName: \"Canon iR-ADV C5045/5051\"\n")
	writeTestPPD(t, c3226, "*NickName: \"Canon iR-ADV C3226\"\n")

	cat := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {c5045, c3226}}}

	// Before nickname caching: neither cryptic filename fuzzy-matches the
	// friendly model text at all, so narrowing correctly finds nothing.
	before := OpenPrintingCandidates(cat, "Canon", "iR-ADV C5045/5051", "")
	if len(before) != 0 {
		t.Fatalf("before nickname caching: OpenPrintingCandidates = %v, want none (no real candidate to choose from yet)", before)
	}

	macRoot := filepath.Join(dir, "macOS")
	nickNames, _ := BuildOpenPrintingNickNames(cat, macRoot, true)
	cat.OpenPrintingNickNames = nickNames

	after := OpenPrintingCandidateDetails(cat, "Canon", "iR-ADV C5045/5051", "")
	if len(after) != 1 || after[0].Path != c5045 {
		t.Fatalf("after nickname caching: OpenPrintingCandidateDetails = %v, want exactly the one PPD whose real *NickName matches (%s)", after, c5045)
	}
}

// TestBuildOpenPrintingNickNames_PersistsAndReusesCacheOnUnchangedFile
// proves a second build reuses the persisted cache rather than re-parsing
// an unchanged PPD - confirmed by corrupting the persisted NickName
// directly (bypassing ReadPPDNickName entirely) and observing the
// corrupted value come back, which could only happen if the cache, not a
// fresh re-parse, answered.
func TestBuildOpenPrintingNickNames_PersistsAndReusesCacheOnUnchangedFile(t *testing.T) {
	dir := t.TempDir()
	ppdPath := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC5045ZU.ppd")
	writeTestPPD(t, ppdPath, "*NickName: \"Canon iR-ADV C5045/5051\"\n")
	cat := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {ppdPath}}}
	macRoot := filepath.Join(dir, "macOS")

	first, firstReports := BuildOpenPrintingNickNames(cat, macRoot, true)
	if first["Canon"][ppdPath] != "Canon iR-ADV C5045/5051" {
		t.Fatalf("expected a real NickName parsed on the first build, got %q", first["Canon"][ppdPath])
	}
	if len(firstReports) != 1 || firstReports[0].Manufacturer != "Canon" || len(firstReports[0].Added) != 1 || firstReports[0].Unchanged != 0 {
		t.Fatalf("expected one report for Canon with one Added entry and none Unchanged on the first build, got %+v", firstReports)
	}

	catalogPath := filepath.Join(macRoot, "OpenPrinting", "Canon", CatalogFileName("Canon"))
	if _, err := os.Stat(catalogPath); err != nil {
		t.Fatalf("expected the catalog file to be persisted, got: %v", err)
	}

	persisted := LoadOpenPrintingCatalog(catalogPath)
	for key, entry := range persisted.Entries {
		entry.NickName = "stale-cached-value-should-still-be-used"
		persisted.Entries[key] = entry
	}
	if err := SaveOpenPrintingCatalog(catalogPath, persisted); err != nil {
		t.Fatalf("SaveOpenPrintingCatalog: %v", err)
	}

	second, secondReports := BuildOpenPrintingNickNames(cat, macRoot, true)
	if len(secondReports) != 1 || len(secondReports[0].Added) != 0 || secondReports[0].Unchanged != 1 {
		t.Errorf("expected the second build's report to show the one PPD as Unchanged (reused from cache), got %+v", secondReports)
	}
	if got := second["Canon"][ppdPath]; got != "stale-cached-value-should-still-be-used" {
		t.Errorf("expected the cached (even if stale) NickName to be reused since the PPD file itself hasn't changed, got %q", got)
	}
}

// TestBuildOpenPrintingNickNames_RereadsAfterFileChanges proves a changed
// PPD (new mtime/size) is re-parsed rather than trusting a stale cache.
func TestBuildOpenPrintingNickNames_RereadsAfterFileChanges(t *testing.T) {
	dir := t.TempDir()
	ppdPath := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC5045ZU.ppd")
	writeTestPPD(t, ppdPath, "*NickName: \"Canon iR-ADV C5045/5051\"\n")
	cat := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {ppdPath}}}
	macRoot := filepath.Join(dir, "macOS")

	first, _ := BuildOpenPrintingNickNames(cat, macRoot, true)
	if first["Canon"][ppdPath] == "" {
		t.Fatal("expected a real NickName from the first parse")
	}

	newer := time.Now().Add(1 * time.Hour)
	if err := os.WriteFile(ppdPath, []byte("*NickName: \"Changed Model Name\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(ppdPath, newer, newer); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	second, secondReports := BuildOpenPrintingNickNames(cat, macRoot, true)
	if got := second["Canon"][ppdPath]; got != "Changed Model Name" {
		t.Errorf("expected a fresh re-parse to pick up the changed content, got %q", got)
	}
	if len(secondReports) != 1 || secondReports[0].Added["macOS/OpenPrinting/Canon/CNPZUIRAC5045ZU.ppd"] != "Changed Model Name" {
		t.Errorf("expected the second report to record this path as Added with its new NickName, got %+v", secondReports)
	}
}

// TestBuildOpenPrintingNickNames_ReportsRemovedPath proves a PPD that
// disappears between two builds (deleted, or moved out of
// scanOpenPrintingPPDs' own scan), while at least one other PPD for the
// same manufacturer still remains, is reported as Removed - the Debug-level
// detail a technician would want ("which old make/models were removed").
func TestBuildOpenPrintingNickNames_ReportsRemovedPath(t *testing.T) {
	dir := t.TempDir()
	c5045 := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC5045ZU.ppd")
	c3226 := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC3226ZU.ppd")
	writeTestPPD(t, c5045, "*NickName: \"Canon iR-ADV C5045/5051\"\n")
	writeTestPPD(t, c3226, "*NickName: \"Canon iR-ADV C3226\"\n")
	macRoot := filepath.Join(dir, "macOS")

	cat := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {c5045, c3226}}}
	if _, reports := BuildOpenPrintingNickNames(cat, macRoot, true); len(reports) != 1 || len(reports[0].Added) != 2 {
		t.Fatalf("expected the first build to record both PPDs as Added, got %+v", reports)
	}

	// c3226 is gone from this run's set entirely (deleted, or moved out of
	// scanOpenPrintingPPDs' own scan) - only c5045 remains.
	catAfterRemoval := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {c5045}}}
	_, reports := BuildOpenPrintingNickNames(catAfterRemoval, macRoot, true)
	if len(reports) != 1 {
		t.Fatalf("expected one report for Canon, got %+v", reports)
	}
	wantRemoved := "macOS/OpenPrinting/Canon/CNPZUIRAC3226ZU.ppd"
	if len(reports[0].Removed) != 1 || reports[0].Removed[0] != wantRemoved {
		t.Errorf("expected Removed = [%q], got %v", wantRemoved, reports[0].Removed)
	}
	if reports[0].Unchanged != 1 {
		t.Errorf("expected the still-present c5045 PPD to be reported Unchanged, got %+v", reports[0])
	}
}

// TestBuildOpenPrintingNickNames_NoPersistStillReturnsCorrectResult proves
// persist=false (a write-protected flash drive - the same contract
// BuildMacModelIndex's own persist parameter already has) still returns a
// fully correct in-memory result, it just skips the disk write.
func TestBuildOpenPrintingNickNames_NoPersistStillReturnsCorrectResult(t *testing.T) {
	dir := t.TempDir()
	ppdPath := filepath.Join(dir, "macOS", "OpenPrinting", "Canon", "CNPZUIRAC5045ZU.ppd")
	writeTestPPD(t, ppdPath, "*NickName: \"Canon iR-ADV C5045/5051\"\n")
	cat := MacCatalog{OpenPrintingPPDs: map[string][]string{"Canon": {ppdPath}}}
	macRoot := filepath.Join(dir, "macOS")

	got, _ := BuildOpenPrintingNickNames(cat, macRoot, false)
	if got["Canon"][ppdPath] != "Canon iR-ADV C5045/5051" {
		t.Fatalf("expected a correct in-memory NickName even with persist=false, got %q", got["Canon"][ppdPath])
	}
	catalogPath := filepath.Join(macRoot, "OpenPrinting", "Canon", CatalogFileName("Canon"))
	if _, err := os.Stat(catalogPath); err == nil {
		t.Error("expected no catalog file to be written when persist is false")
	}
}
