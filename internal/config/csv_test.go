package config

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTemplate_HeaderOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Printers.csv")
	if err := WriteTemplate(path); err != nil {
		t.Fatalf("WriteTemplate: %v", err)
	}
	rows, err := ImportCSV(path)
	if err != nil {
		t.Fatalf("ImportCSV of a fresh template: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 data rows in a fresh template, got %d", len(rows))
	}
}

func TestImportCSV_QuotesCommaOnlyWhenNeeded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Printers.csv")
	// "Copy Room, West" deliberately contains a comma and must be quoted to
	// stay one field; every other value here has no comma and is left bare -
	// exactly the "quote only when needed" round-trip this format requires.
	content := "Name,IP,Manufacturer,Model,Driver,SNMP,Mono,1-sided,UseExistingPort,AdvancedPrintingFeatures\n" +
		"\"Copy Room, West\",10.1.1.50,Canon,,Canon Generic Plus UFR II,true,false,true,false,false\n" +
		"Front Desk,10.1.1.51,HP,,HP Universal Printing PCL 6,yes,no,1,off,\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rows, err := ImportCSV(path)
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "Copy Room, West" {
		t.Errorf("Name = %q, want %q (the comma must stay inside one field)", rows[0].Name, "Copy Room, West")
	}
	if rows[0].IP != "10.1.1.50" {
		t.Errorf("IP = %q, want 10.1.1.50 (quoting the previous field must not shift columns)", rows[0].IP)
	}
	if !rows[0].SNMP || rows[0].Mono || !rows[0].OneSided || rows[0].UseExistingPort {
		t.Errorf("row 0 booleans = %+v, want SNMP=true Mono=false OneSided=true UseExistingPort=false", rows[0])
	}
	if rows[1].Name != "Front Desk" {
		t.Errorf("Name = %q, want 'Front Desk'", rows[1].Name)
	}
}

func TestCsvWriter_QuotesOnlyWhenNeeded(t *testing.T) {
	// Confirms the exact building block WriteTemplate/ImportCSV are built on
	// (encoding/csv.Writer) does the requested quoting on its own: a comma
	// forces quotes on that field only, plain values never get quoted, with
	// zero extra configuration - PDT's CSV code deliberately never hand-rolls
	// this itself.
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"A, B", "1.2.3.4", "Plain"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.Flush()

	s := buf.String()
	if !strings.Contains(s, `"A, B"`) {
		t.Errorf("expected the comma-containing field to be quoted: %q", s)
	}
	if strings.Contains(s, `"Plain"`) || strings.Contains(s, `"1.2.3.4"`) {
		t.Errorf("plain fields without commas should never be quoted: %q", s)
	}
}
