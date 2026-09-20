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
	// GitHub issue #16's own MacDriver/MacEnabled/WindowsDisabled keys are
	// completely absent from this fixture (it predates all three) -
	// PreDatesMacDriverSplit must report that, not just default zero values,
	// so app.go's own OpenConfiguration can correctly migrate a genuinely
	// old mac-authored row's single Driver value into MacDriver.
	if !row.PreDatesMacDriverSplit {
		t.Error("expected PreDatesMacDriverSplit=true for a file with none of the new keys at all")
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

// TestSaveLoadConfig_RoundTrip_NotPreDatesMacDriverSplit is the direct
// regression test for the real false-positive risk PreDatesMacDriverSplit's
// own doc comment describes: a perfectly ordinary row saved by a current
// build - Model and a Windows Driver both filled in, macOS left deliberately
// unchecked - must never be mistaken for a genuinely pre-issue-#16 file just
// because MacDriver/MacEnabled happen to still be at their zero values.
func TestSaveLoadConfig_RoundTrip_NotPreDatesMacDriverSplit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := SavedConfig{
		SalesChainID: "12345",
		Printers: []SavedRow{
			RowFromPrinterRow(printer.PrinterRow{
				Name: "Front Desk", IP: "10.1.1.50", Manufacturer: "Canon", Model: "imageFORCE C331F",
				Driver: "Canon Generic Plus UFR II",
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
	if loaded.Printers[0].PreDatesMacDriverSplit {
		t.Error("expected PreDatesMacDriverSplit=false for a row saved by a build that already knows about MacDriver/MacEnabled, even with both left at their zero value")
	}
}

// TestSavedRow_CarriesModelAndBothDrivers confirms Save Configuration's own
// JSON document captures Model plus both platforms' own driver commitments
// for a single row - Ken's own explicit ask, since the whole point of
// splitting Driver/MacDriver is a row authored once (typically on Windows)
// carrying both, not just whichever platform happens to be running right now.
func TestSavedRow_CarriesModelAndBothDrivers(t *testing.T) {
	row := RowFromPrinterRow(printer.PrinterRow{
		Name: "Front Desk", Manufacturer: "Canon", Model: "imageFORCE C331F",
		Driver: "Canon Generic Plus UFR II", MacDriver: "imageFORCE C331F (UFR II)", MacEnabled: true,
	}, true)
	if row.Model != "imageFORCE C331F" {
		t.Errorf("Model = %q, want %q", row.Model, "imageFORCE C331F")
	}
	if row.Driver != "Canon Generic Plus UFR II" {
		t.Errorf("Driver = %q, want the Windows driver name", row.Driver)
	}
	if row.MacDriver != "imageFORCE C331F (UFR II)" {
		t.Errorf("MacDriver = %q, want the macOS driver commitment", row.MacDriver)
	}
	if !row.MacEnabled {
		t.Error("expected MacEnabled to round-trip as true")
	}

	back := row.ToPrinterRow()
	if back.Model != row.Model || back.Driver != row.Driver || back.MacDriver != row.MacDriver || back.MacEnabled != row.MacEnabled {
		t.Errorf("ToPrinterRow() = %+v, did not carry Model/Driver/MacDriver/MacEnabled through", back)
	}
}
