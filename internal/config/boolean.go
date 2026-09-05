package config

import "strings"

// ParseCsvBool ports ConvertTo-CsvBoolean: blank, or one of 0/false/no/n/off
// (trimmed, case-insensitive), is false; anything else is true.
func ParseCsvBool(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "n", "off":
		return false
	default:
		return true
	}
}
