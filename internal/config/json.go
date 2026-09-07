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
	Select                   bool   `json:"Select"`
	Name                     string `json:"Name"`
	IP                       string `json:"IP"`
	Manufacturer             string `json:"Manufacturer"`
	Model                    string `json:"Model"`
	Driver                   string `json:"Driver"`
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
}

func (r SavedRow) ToPrinterRow() printer.PrinterRow {
	return printer.PrinterRow{
		Name: r.Name, IP: r.IP, Manufacturer: r.Manufacturer, Model: r.Model, Driver: r.Driver,
		SNMP: r.SNMP, SNMPCommunity: r.SNMPCommunity, Mono: r.Mono, OneSided: r.OneSided,
		UseExistingPort: r.UseExistingPort, AdvancedPrintingFeatures: r.AdvancedPrintingFeatures,
		DevModeFile: r.DevModeFile,
	}
}

func RowFromPrinterRow(row printer.PrinterRow, selected bool) SavedRow {
	return SavedRow{
		Select: selected, Name: row.Name, IP: row.IP, Manufacturer: row.Manufacturer, Model: row.Model,
		Driver: row.Driver, SNMP: row.SNMP, SNMPCommunity: row.SNMPCommunity, Mono: row.Mono, OneSided: row.OneSided,
		UseExistingPort: row.UseExistingPort, AdvancedPrintingFeatures: row.AdvancedPrintingFeatures,
		DevModeFile: row.DevModeFile,
	}
}

// SavedConfig is the whole Open/Save Configuration document shape.
type SavedConfig struct {
	SalesChainID string     `json:"SalesChainId"`
	Printers     []SavedRow `json:"Printers"`
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
	return cfg, nil
}

func SaveConfig(path string, cfg SavedConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
