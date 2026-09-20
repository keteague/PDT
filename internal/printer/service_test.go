package printer

import "testing"

func TestRequiresNulPortWorkaround(t *testing.T) {
	// True for every manufacturer/driver now (Ken, 2026-09-20) - including
	// blanks, since the Windows deploy path treats it as the default.
	cases := []struct{ mfg, driver string }{
		{"HP", "HP Universal Printing PCL 6"},
		{"HP", "HP LaserJet Pro M404"},
		{"Canon", "Canon Generic Plus UFR II"},
		{"Ricoh", "RICOH PCL6 UniversalDriver V4.45"},
		{"Kyocera", "Kyocera TASKalfa MZ6001ci KX"},
		{"Lexmark", "Lexmark Universal v2 XL"},
		{"Xerox", "Xerox GPD PCL6 V5.1076.4.0"},
		{"Toshiba", "TOSHIBA Universal Printer 2"},
		{"Sharp", "SHARP UD3 PCL6"},
		{"Konica Minolta", "KONICA MINOLTA Universal PCL"},
		{"", ""},
	}
	for _, c := range cases {
		if !RequiresNulPortWorkaround(c.mfg, c.driver) {
			t.Errorf("RequiresNulPortWorkaround(%q, %q) = false, want true", c.mfg, c.driver)
		}
	}
}

func TestNormalizeIP(t *testing.T) {
	cases := []struct {
		raw       string
		wantValue string
		wantNul   bool
		wantErr   bool
	}{
		{"", "", false, true},
		{"   ", "", false, true},
		{"NUL", NulPortName, true, false},
		{"nul", NulPortName, true, false},
		{"NUL:", NulPortName, true, false},
		{"  nul:  ", NulPortName, true, false},
		{"10.1.1.50", "10.1.1.50", false, false},
		{"  10.1.1.50  ", "10.1.1.50", false, false},
		{"printer.example.com", "printer.example.com", false, false},
	}
	for _, c := range cases {
		value, isNul, err := NormalizeIP(c.raw)
		if (err != nil) != c.wantErr {
			t.Errorf("NormalizeIP(%q) err = %v, wantErr %v", c.raw, err, c.wantErr)
			continue
		}
		if err != nil {
			continue
		}
		if value != c.wantValue || isNul != c.wantNul {
			t.Errorf("NormalizeIP(%q) = (%q, %v), want (%q, %v)", c.raw, value, isNul, c.wantValue, c.wantNul)
		}
	}
}
