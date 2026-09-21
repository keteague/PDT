package main

import (
	"strings"
	"testing"

	"PDT/internal/driver"
)

// HP has no macOS model index and no per-model Windows data - its Model list
// comes from OpenPrinting's own real model names (Ken, 2026-09-20), cleaned
// of language suffixes, each with a tooltip naming its PPD.
func TestModelCandidatesWithSource_OpenPrintingSuppliesModelsForHP(t *testing.T) {
	p := "/drivers/macOS/OpenPrinting/HP/hp-laserjet_m404.ppd.gz"
	cat := driver.MacCatalog{
		OpenPrintingPPDs:      map[string][]string{"HP": {p}},
		OpenPrintingNickNames: map[string]map[string]string{"HP": {p: "HP LaserJet Pro M404 PCL"}},
	}
	got := modelCandidatesWithSource(cat, driver.MacModelIndex{}, nil, nil, "HP", "")
	if len(got) != 1 || got[0].Label != "HP LaserJet Pro M404" {
		t.Fatalf("expected the cleaned OpenPrinting model, got %+v", got)
	}
	if !strings.Contains(got[0].Source, "hp-laserjet_m404.ppd.gz") {
		t.Errorf("tooltip should name the PPD, got %q", got[0].Source)
	}
}

// One model offered by several driver versions is one entry whose tooltip
// lists every package - the Xerox "same model, different versions" case.
func TestModelCandidatesWithSource_MergesSourcesOfOneModel(t *testing.T) {
	idx := driver.MacModelIndex{"Xerox": {"Xerox AltaLink B8045": {
		{SourcePackagePath: "/d/macOS/Xerox/A/XeroxDrivers_5.10.1.dmg"},
		{SourcePackagePath: "/d/macOS/Xerox/B/XeroxDrivers_5.19.3.dmg"},
	}}}
	got := modelCandidatesWithSource(driver.MacCatalog{}, idx, nil, nil, "Xerox", "")
	if len(got) != 1 || strings.Count(got[0].Source, "\n") != 1 {
		t.Fatalf("expected one model listing both packages, got %+v", got)
	}
}

// TestModelCandidatesWithSource_ExcludesJapanMarketAndFFPSVariants guards
// two real, reported bugs: OpenPrinting's own raw PPD nicknames include
// Japan-market-only SKUs and Xerox's own FFPS print-server variant, neither
// of which indexFamilyPackage's own local-package path ever has to filter
// (see IsExcludedModelVariant's own doc comment) - this OpenPrinting path
// needs the identical exclusion applied itself.
func TestModelCandidatesWithSource_ExcludesJapanMarketAndFFPSVariants(t *testing.T) {
	jp := "/drivers/macOS/OpenPrinting/Ricoh/ricoh-im-c3510-jpn.ppd.gz"
	plain := "/drivers/macOS/OpenPrinting/Ricoh/ricoh-im-c3510.ppd.gz"
	cat := driver.MacCatalog{
		OpenPrintingPPDs: map[string][]string{"Ricoh": {jp, plain}},
		OpenPrintingNickNames: map[string]map[string]string{"Ricoh": {
			jp:    "RICOH IM C3510 JPN",
			plain: "RICOH IM C3510",
		}},
	}
	got := modelCandidatesWithSource(cat, driver.MacModelIndex{}, nil, nil, "Ricoh", "")
	if len(got) != 1 || got[0].Label != "Ricoh IM C3510" {
		t.Fatalf("expected only the non-JPN model, canonicalized to the manufacturer's own casing, got %+v", got)
	}
}

// TestModelCandidatesWithSource_DedupsManufacturerPrefixCase guards the
// other real, reported Ricoh bug: OpenPrinting spells "Ricoh" inconsistently
// across its own PPDs ("RICOH ..." vs "Ricoh ..." for the identical model),
// which must collapse into one dropdown entry, not two.
func TestModelCandidatesWithSource_DedupsManufacturerPrefixCase(t *testing.T) {
	upper := "/drivers/macOS/OpenPrinting/Ricoh/a.ppd.gz"
	mixed := "/drivers/macOS/OpenPrinting/Ricoh/b.ppd.gz"
	cat := driver.MacCatalog{
		OpenPrintingPPDs: map[string][]string{"Ricoh": {upper, mixed}},
		OpenPrintingNickNames: map[string]map[string]string{"Ricoh": {
			upper: "RICOH IM C3510",
			mixed: "Ricoh IM C3510",
		}},
	}
	got := modelCandidatesWithSource(cat, driver.MacModelIndex{}, nil, nil, "Ricoh", "")
	if len(got) != 1 || got[0].Label != "Ricoh IM C3510" {
		t.Fatalf("expected the two case variants to dedup into one entry, got %+v", got)
	}
	if !strings.Contains(got[0].Source, "a.ppd.gz") || !strings.Contains(got[0].Source, "b.ppd.gz") {
		t.Errorf("tooltip should still list both real PPDs, got %q", got[0].Source)
	}
}

func TestMacDriverProblem(t *testing.T) {
	idx := driver.MacModelIndex{"Kyocera": {"CS 2554ci": {{Label: "CS 2554ci (Driver)", SourcePackagePath: "/d/k.dmg"}}}}
	cat := driver.MacCatalog{}
	if p := macDriverProblem(cat, idx, "Kyocera", "CS 2554ci", "CS 2554ci (Driver)"); p != "" {
		t.Errorf("valid pick reported a problem: %s", p)
	}
	if p := macDriverProblem(cat, idx, "Kyocera", "CS 2554ci", driver.GenericPostScriptLabel); p != "" {
		t.Errorf("Apple Generic PostScript is always valid, got: %s", p)
	}
	if p := macDriverProblem(cat, idx, "Kyocera", "CS 2554ci", "Kyocera Web build 2026.07.03"); p == "" {
		t.Error("a package name is not a driver - expected a problem")
	}
	if p := macDriverProblem(cat, idx, "Kyocera", "No Such Model 1", ""); p == "" {
		t.Error("a model missing from the macOS catalog should be flagged")
	}
}

func TestWindowsDriverProblem(t *testing.T) {
	cat := driver.Catalog{}
	if p := windowsDriverProblem(cat, nil, "Toshiba", ""); p == "" {
		t.Error("a manufacturer with no Windows drivers at all should be flagged even with a blank driver")
	}
}
