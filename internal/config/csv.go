package config

import (
	"encoding/csv"
	"os"
	"strings"

	"PDT/internal/printer"
)

// CsvHeader is the New CSV template's header row, and the column set
// ImportCSV matches by name (order-independent, case-insensitive). There is
// no Select column (matches the original: Import-PrinterCsv never read one -
// every imported row just defaults to selected) and no BindNulPort/NUL column
// (removed entirely; see printer.NormalizeIP and printer.RequiresNulPortWorkaround
// for what replaced it).
var CsvHeader = []string{"Name", "IP", "Manufacturer", "Model", "Driver",
	"SNMP", "Mono", "1-sided", "UseExistingPort", "AdvancedPrintingFeatures"}

// WriteTemplate creates path and writes just CsvHeader as its only row - the
// entire backend of the "New CSV" button, before the user fills in any rows
// externally in a spreadsheet app.
func WriteTemplate(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write(CsvHeader); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

// ImportCSV reads a populated CSV into rows, matching columns by header name.
// encoding/csv already implements RFC4180 quoting on both read and write with
// no extra configuration: a field is quoted only if it contains a comma (or a
// quote/newline), and a quoted field's embedded comma doesn't split into an
// extra column on read - exactly the round-trip behavior requested, and
// shared with WriteTemplate above.
func ImportCSV(path string) ([]printer.PrinterRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate short/ragged rows rather than erroring
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(rec []string, name string) string {
		i, ok := col[strings.ToLower(name)]
		if !ok || i >= len(rec) {
			return ""
		}
		return rec[i]
	}

	// []printer.PrinterRow{}, not "var rows []printer.PrinterRow" (a nil
	// slice) - a nil slice marshals to JSON `null`, and the frontend calls
	// .map()/.length on result.rows with no defensive fallback (see
	// driver.ManufacturersWithDrivers' own comment for the exact same class
	// of bug) - a CSV with a header row but zero data rows is a routine,
	// valid "imported nothing" result, not an error.
	rows := []printer.PrinterRow{}
	for _, rec := range records[1:] {
		rows = append(rows, printer.PrinterRow{
			Name:                     strings.TrimSpace(get(rec, "Name")),
			IP:                       strings.TrimSpace(get(rec, "IP")),
			Manufacturer:             strings.TrimSpace(get(rec, "Manufacturer")),
			Model:                    strings.TrimSpace(get(rec, "Model")),
			Driver:                   strings.TrimSpace(get(rec, "Driver")),
			SNMP:                     ParseCsvBool(get(rec, "SNMP")),
			Mono:                     ParseCsvBool(get(rec, "Mono")),
			OneSided:                 ParseCsvBool(get(rec, "1-sided")),
			UseExistingPort:          ParseCsvBool(get(rec, "UseExistingPort")),
			AdvancedPrintingFeatures: ParseCsvBool(get(rec, "AdvancedPrintingFeatures")),
		})
	}
	return rows, nil
}
