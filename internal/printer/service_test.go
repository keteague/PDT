package printer

import "testing"

func TestRequiresNulPortWorkaround(t *testing.T) {
	cases := []struct {
		mfg, driver string
		want        bool
	}{
		{"HP", "HP Universal Printing PCL 6", true},
		{"HP", "HP Universal Printing PS", true},
		{"hp", "hp universal printing pcl 6", true}, // case-insensitive
		{"HP", "HP LaserJet Pro M404", false},
		{"Canon", "Canon Generic Plus UFR II", false},
		{"Kyocera", "Kyocera FS-1100 KX", false},
		{"HP", "", false},
		{"", "HP Universal Printing PCL 6", false},
	}
	for _, c := range cases {
		if got := RequiresNulPortWorkaround(c.mfg, c.driver); got != c.want {
			t.Errorf("RequiresNulPortWorkaround(%q, %q) = %v, want %v", c.mfg, c.driver, got, c.want)
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
