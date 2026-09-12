package config

import (
	"os"
	"path/filepath"
	"testing"

	"PDT/internal/printer"
)

// oldFormatFixture mirrors exactly what Create-Printers.ps1's
// Get-PrinterRowsForExport + ConvertTo-Json produced (confirmed via reading
// the script: Select, Name, IP, Manufacturer, Model, Driver, Snmp, Mono,
// OneSided, UseExistingPort, BindNulPort, AdvancedPrintingFeatures), so
// loading it proves an old PowerShell-saved config still opens in PDT
// unmodified even though BindNulPort no longer exists as a concept here.
const oldFormatFixture = `{
  "SalesChainId": "18465",
  "Printers": [
    {
      "Select": true,
      "Name": "Front Desk",
      "IP": "10.1.1.50",
      "Manufacturer": "HP",
      "Model": "",
      "Driver": "HP Universal Printing PCL 6",
      "Snmp": true,
      "Mono": false,
      "OneSided": true,
      "UseExistingPort": false,
      "BindNulPort": true,
      "AdvancedPrintingFeatures": false
    }
  ]
}`

func TestLoadConfig_OldPowerShellFormatIsCompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(path, []byte(oldFormatFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig should tolerate an unknown legacy BindNulPort key, got error: %v", err)
	}
	if cfg.SalesChainID != "18465" {
		t.Errorf("SalesChainID = %q, want 18465", cfg.SalesChainID)
	}
	if len(cfg.Printers) != 1 {
		t.Fatalf("expected 1 printer, got %d", len(cfg.Printers))
	}
	row := cfg.Printers[0]
	if !row.Select || row.Name != "Front Desk" || row.IP != "10.1.1.50" || row.Manufacturer != "HP" ||
		row.Driver != "HP Universal Printing PCL 6" || !row.SNMP || row.Mono || !row.OneSided {
		t.Errorf("row = %+v, did not round-trip as expected from the legacy format", row)
	}
}

func TestSaveLoadConfig_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := SavedConfig{
		SalesChainID: "12345",
		Printers: []SavedRow{
			RowFromPrinterRow(printer.PrinterRow{
				Name: "Front Desk", IP: "10.1.1.50", LPDQueueName: "raw", Manufacturer: "HP",
				Driver: "HP Universal Printing PCL 6", SNMP: true, OneSided: true,
			}, true),
		},
	}
	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if loaded.SalesChainID != original.SalesChainID || len(loaded.Printers) != len(original.Printers) {
		t.Errorf("round trip mismatch: got %+v, want %+v", loaded, original)
	}
	// LPDQueueName round-trips even though this saves/loads on Windows too,
	// which never reads it - confirmed this way so a config saved on Windows
	// already carries the right value the moment it's opened on a Mac (see
	// printer.PrinterRow's own doc comment).
	if got := loaded.Printers[0].LPDQueueName; got != "raw" {
		t.Errorf("loaded.Printers[0].LPDQueueName = %q, want %q", got, "raw")
	}
	if got := loaded.Printers[0].ToPrinterRow().LPDQueueName; got != "raw" {
		t.Errorf("ToPrinterRow().LPDQueueName = %q, want %q", got, "raw")
	}
}
