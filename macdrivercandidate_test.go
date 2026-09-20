package main

import (
	"strings"
	"testing"
	"time"

	"PDT/internal/driver"
)

func containsCandidateLabel(candidates []MacDriverCandidate, label string) bool {
	for _, c := range candidates {
		if c.Label == label {
			return true
		}
	}
	return false
}

// TestMacDriverCandidatesWithSource_NoOpenPrintingMatchSkipsFullList is the
// direct regression test for Ken's own explicit ask (2026-09-19): "if there
// is no OpenPrinting driver candidate, we should not show the complete list
// of OpenPrinting drivers for selection." Ricoh has no macFamilyPreference
// table and no local installer package here, so the only thing that could
// possibly resolve is OpenPrinting - two PPDs with cryptic filenames and no
// cached NickName at all (OpenPrintingNickNames left nil, simulating a
// build that hasn't cataloged them yet) mean neither one fuzzy-matches the
// friendly model text, so the result must not contain either of them.
func TestMacDriverCandidatesWithSource_NoOpenPrintingMatchSkipsFullList(t *testing.T) {
	catalog := driver.MacCatalog{
		OpenPrintingPPDs: map[string][]string{
			"Ricoh": {"/drivers/OpenPrinting/Ricoh/rcadv1.ppd", "/drivers/OpenPrinting/Ricoh/rcadv2.ppd"},
		},
	}
	got := macDriverCandidatesWithSource(catalog, driver.MacModelIndex{}, "Ricoh", "MP C3003", "")
	if containsCandidateLabel(got, "rcadv1 (OP)") || containsCandidateLabel(got, "rcadv2 (OP)") {
		t.Errorf("expected neither unmatched OpenPrinting PPD to appear, got %+v", got)
	}
}

// TestMacDriverCandidatesWithSource_GenericPostScriptAlwaysPresent is the
// direct regression test for Ken's other explicit ask, same message: "I
// think the Apple Generic PostScript driver should always be an option" -
// not just a last resort when nothing else resolves. HP has a real local
// installer package here (ResolveMac's own path, no macFamilyPreference
// table), so a real candidate does resolve - Generic PostScript must still
// show up alongside it.
func TestMacDriverCandidatesWithSource_GenericPostScriptAlwaysPresent(t *testing.T) {
	catalog := driver.MacCatalog{
		Packages: map[string][]driver.MacPackage{
			"HP": {{Path: "/drivers/HP/HP_LaserJet.pkg", Kind: driver.MacPackagePkg, ModTime: time.Now()}},
		},
	}
	got := macDriverCandidatesWithSource(catalog, driver.MacModelIndex{}, "HP", "", "")
	if len(got) < 2 {
		t.Fatalf("expected both the resolved HP package and Generic PostScript, got %+v", got)
	}
	if !containsCandidateLabel(got, driver.GenericPostScriptLabel) {
		t.Errorf("expected %q to always be present, got %+v", driver.GenericPostScriptLabel, got)
	}
}

// TestMacDriverCandidatesWithSource_GenericPostScriptOnlyOptionWhenNothingElseResolves
// confirms the plain "nothing at all resolved" case still works exactly as
// before - a manufacturer with no package, no model index entry, and no
// OpenPrinting PPDs at all still gets Generic PostScript (found on the
// primary path now, not the old full-generic-set fallback, since it always
// matches a blank filterText).
func TestMacDriverCandidatesWithSource_GenericPostScriptOnlyOptionWhenNothingElseResolves(t *testing.T) {
	got := macDriverCandidatesWithSource(driver.MacCatalog{}, driver.MacModelIndex{}, "Toshiba", "", "")
	if !containsCandidateLabel(got, driver.GenericPostScriptLabel) {
		t.Errorf("expected %q, got %+v", driver.GenericPostScriptLabel, got)
	}
}

// TestMacDriverCandidatesWithSource_OpenPrintingShowsNickNameTooltipShowsDriverName
// is the direct regression test for Ken's own explicit ask (2026-09-19):
// "OpenPrinting PPD's should show the nickName rather than the driver name
// in the selection list, and show the driver name in the tooltip." A cached
// NickName (BuildOpenPrintingNickNames) must become the dropdown's own
// primary Label, with the old filename-derived label (what used to be the
// only thing shown) demoted into the tooltip alongside the source path Ken
// separately asked for earlier.
func TestMacDriverCandidatesWithSource_OpenPrintingShowsNickNameTooltipShowsDriverName(t *testing.T) {
	path := "/drivers/OpenPrinting/Canon/cnadvc5045x1g.ppd"
	catalog := driver.MacCatalog{
		OpenPrintingPPDs:      map[string][]string{"Canon": {path}},
		OpenPrintingNickNames: map[string]map[string]string{"Canon": {path: "Canon iR-ADV C5045/5051"}},
	}
	got := macDriverCandidatesWithSource(catalog, driver.MacModelIndex{}, "Canon", "iR-ADV C5045/5051", "")

	var found *MacDriverCandidate
	for i := range got {
		if got[i].Label == "Canon iR-ADV C5045/5051" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatalf("expected the NickName as the primary label, got %+v", got)
	}
	if !strings.Contains(found.Source, "cnadvc5045x1g (OP)") {
		t.Errorf("expected the tooltip to still carry the old filename-derived driver name, got %q", found.Source)
	}
}

// TestMacDriverCandidatesWithSource_OpenPrintingWithoutNickNameUnaffected
// proves a PPD with nothing cached yet (or no *NickName/*ModelName field at
// all) keeps showing exactly what it always did - the filename-derived
// label, plain path in the tooltip - rather than an empty or malformed
// label.
func TestMacDriverCandidatesWithSource_OpenPrintingWithoutNickNameUnaffected(t *testing.T) {
	path := "/drivers/OpenPrinting/Ricoh/Ricoh_MP_C3003.ppd"
	catalog := driver.MacCatalog{OpenPrintingPPDs: map[string][]string{"Ricoh": {path}}}
	got := macDriverCandidatesWithSource(catalog, driver.MacModelIndex{}, "Ricoh", "MP C3003", "")

	if !containsCandidateLabel(got, "Ricoh MP C3003 (OP)") {
		t.Errorf("expected the pre-existing filename-derived label unchanged, got %+v", got)
	}
}
