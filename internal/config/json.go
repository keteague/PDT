package config

import (
	"encoding/json"
	"os"

	"PDT/internal/printer"
)

// SavedRow is one row as it round-trips through Open/Save Configuration.
// Unlike printer.PrinterRow, it carries Select - the grid checkbox state,
// which (per the original tool) persists through JSON save/load but was
// never part of CSV import/export. JSON tags are explicit PascalCase to
// match the field names Create-Printers.ps1's ConvertTo-Json/ConvertFrom-Json
// already used, so a config saved by the original PowerShell tool loads into
// PDT unmodified - a stray "BindNulPort" key in an old file is simply ignored
// by encoding/json, no migration code needed.
type SavedRow struct {
	Select bool `json:"Select"`
	// ID is a free-text, technician-assigned label (e.g. "1-1") for telling
	// apart multiple MFDs of the same make/model at one client site in the
	// Runbook report (GitHub issue #15) - purely a documentation aid with no
	// bearing on Deploy at all, which is why it lives only here (round-tripped
	// through Open/Save Configuration) and not on printer.PrinterRow/CSV -
	// neither Deploy nor a spreadsheet import/export has any use for it.
	// Never required, and deliberately skipped in the grid's own tab order
	// (see the frontend's own row-id input) since most site surveys with a
	// single MFD never need it at all.
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	IP     string `json:"IP"`
	// LPDQueueName: see printer.PrinterRow's own doc comment - a new field
	// with no Create-Printers.ps1 precedent to match, so its JSON key is
	// just its own Go name like everything else added since.
	LPDQueueName string `json:"LPDQueueName"`
	Manufacturer string `json:"Manufacturer"`
	Model        string `json:"Model"`
	// Driver, MacDriver, WindowsDisabled, MacEnabled: see
	// printer.PrinterRow's own doc comments (GitHub issue #16) - identical
	// fields/meanings, just PascalCase-JSON-tagged like everything else here.
	Driver                   string `json:"Driver"`
	MacDriver                string `json:"MacDriver"`
	WindowsDisabled          bool   `json:"WindowsDisabled"`
	MacEnabled               bool   `json:"MacEnabled"`
	SNMP                     bool   `json:"Snmp"`
	SNMPCommunity            string `json:"SnmpCommunity"`
	Mono                     bool   `json:"Mono"`
	OneSided                 bool   `json:"OneSided"`
	UseExistingPort          bool   `json:"UseExistingPort"`
	AdvancedPrintingFeatures bool   `json:"AdvancedPrintingFeatures"`
	// DevModeFile is a pointer (bare filename under the Configs folder) to a
	// captured raw DEVMODE, not the DEVMODE bytes themselves - see
	// printer.PrinterRow's own DevModeFile field and printer.ResolveDevModePath.
	DevModeFile string `json:"DevModeFile"`
	// PreDatesMacDriverSplit is true only when none of MacDriver/MacEnabled/
	// WindowsDisabled's own JSON keys were present in the file this row was
	// loaded from at all (set by LoadConfig, below) - not merely at their
	// zero value, which a perfectly ordinary new row (Model and a Windows
	// Driver both filled in, macOS deliberately left unchecked - an
	// extremely common shape going forward, not a rare edge case) would also
	// have. OpenConfiguration (app.go) uses this signal, together with
	// state.macModelManufacturers, to migrate a genuinely pre-issue-#16
	// mac-authored row's own single Driver value into MacDriver instead of
	// silently losing it - seeing this true is the only reliable way to tell
	// "this predates the split" apart from "this row just hasn't touched
	// macOS yet." Never written back out by SaveConfig in practice: nothing
	// in the frontend's own SaveConfiguration payload ever sets this key, so
	// it's simply absent from every file PDT itself saves going forward -
	// but it's a normal (not "-") JSON tag, not merely a Go-side
	// convenience, since OpenConfiguration needs it to survive is one and
	// only crossing of the Wails JS bridge.
	PreDatesMacDriverSplit bool `json:"preDatesMacDriverSplit"`
}

func (r SavedRow) ToPrinterRow() printer.PrinterRow {
	return printer.PrinterRow{
		Name: r.Name, IP: r.IP, LPDQueueName: r.LPDQueueName, Manufacturer: r.Manufacturer, Model: r.Model, Driver: r.Driver,
		MacDriver: r.MacDriver, WindowsDisabled: r.WindowsDisabled, MacEnabled: r.MacEnabled,
		SNMP: r.SNMP, SNMPCommunity: r.SNMPCommunity, Mono: r.Mono, OneSided: r.OneSided,
		UseExistingPort: r.UseExistingPort, AdvancedPrintingFeatures: r.AdvancedPrintingFeatures,
		DevModeFile: r.DevModeFile,
	}
}

func RowFromPrinterRow(row printer.PrinterRow, selected bool) SavedRow {
	return SavedRow{
		Select: selected, Name: row.Name, IP: row.IP, LPDQueueName: row.LPDQueueName, Manufacturer: row.Manufacturer, Model: row.Model,
		Driver: row.Driver, MacDriver: row.MacDriver, WindowsDisabled: row.WindowsDisabled, MacEnabled: row.MacEnabled,
		SNMP: row.SNMP, SNMPCommunity: row.SNMPCommunity, Mono: row.Mono, OneSided: row.OneSided,
		UseExistingPort: row.UseExistingPort, AdvancedPrintingFeatures: row.AdvancedPrintingFeatures,
		DevModeFile: row.DevModeFile,
	}
}

// SavedConfig is the whole Open/Save Configuration document shape.
type SavedConfig struct {
	SalesChainID string     `json:"SalesChainId"`
	Printers     []SavedRow `json:"Printers"`
}

// rawPrinterKeys mirrors just enough of SavedConfig's own shape to check
// which keys a saved row's JSON object actually had, independent of
// json.Unmarshal's own "missing key -> zero value" behavior - see
// SavedRow.PreDatesMacDriverSplit's own doc comment for why LoadConfig needs
// this at all.
type rawPrinterKeys struct {
	Printers []map[string]json.RawMessage `json:"Printers"`
}

func LoadConfig(path string) (SavedConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SavedConfig{}, err
	}
	var cfg SavedConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return SavedConfig{}, err
	}

	var raw rawPrinterKeys
	if err := json.Unmarshal(data, &raw); err == nil {
		for i := range cfg.Printers {
			if i >= len(raw.Printers) {
				break
			}
			_, hasMacDriver := raw.Printers[i]["MacDriver"]
			_, hasMacEnabled := raw.Printers[i]["MacEnabled"]
			_, hasWindowsDisabled := raw.Printers[i]["WindowsDisabled"]
			cfg.Printers[i].PreDatesMacDriverSplit = !hasMacDriver && !hasMacEnabled && !hasWindowsDisabled
		}
	}
	return cfg, nil
}

func SaveConfig(path string, cfg SavedConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
