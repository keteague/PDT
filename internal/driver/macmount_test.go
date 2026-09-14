package driver

import "testing"

// TestIsDmgLikePath locks in the real finding (2026-09-13) behind Toshiba's
// support: its own real download ("TOSHIBA_ColorMFP.dmg.gz") is a plain
// gzip-compressed UDIF image, a shape none of Canon/Kyocera/Ricoh/Sharp/
// Xerox's own real downloads have - confirmed live that `hdiutil attach`
// does NOT auto-detect a bare gzip wrapper on its own ("image not
// recognized"), so isDmgLikePath/mountDmg needed to handle it explicitly.
func TestIsDmgLikePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"XeroxDrivers_5.19.3_2562.dmg", true},
		{"XEROXDRIVERS_5.19.3_2562.DMG", true},
		{"TOSHIBA_ColorMFP.dmg.gz", true},
		{"TOSHIBA_COLORMFP.DMG.GZ", true},
		{"UFRII_v10.19.25_mac.pkg", false},
		{"Generic_GUC_PrinterSoftware_11202025.dmg.zip", false},
		{"plain.gz", false},
		{"noextensionatall", false},
	}
	for _, c := range cases {
		if got := isDmgLikePath(c.path); got != c.want {
			t.Errorf("isDmgLikePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestPackageLabel_StripsBothSuffixesForDmgGz guards the real, confirmed
// finding that PackageLabel's own filename fallback (reached here since
// pkgutil has nothing real to expand at this fake path) only stripped the
// trailing ".gz" off a compound ".dmg.gz" name via a single Ext-based trim,
// leaving ".dmg" in the displayed label ("TOSHIBA_ColorMFP.dmg" instead of
// "TOSHIBA_ColorMFP"). Deliberately checked against a real Xerox-shaped
// filename with a legitimate dot in its own version number too, guarding
// the fix stays scoped to the ".dmg.gz" shape specifically - a second,
// blind Ext-based strip would have wrongly cut "XeroxDrivers_5.19" down
// from "XeroxDrivers_5.19.3_2562", losing real version digits.
func TestPackageLabel_StripsBothSuffixesForDmgGz(t *testing.T) {
	if got := PackageLabel("/nonexistent/TOSHIBA_ColorMFP.dmg.gz"); got != "TOSHIBA_ColorMFP" {
		t.Errorf(`PackageLabel(".dmg.gz") = %q, want "TOSHIBA_ColorMFP"`, got)
	}
	if got := PackageLabel("/nonexistent/XeroxDrivers_5.19.3_2562.dmg"); got != "XeroxDrivers_5.19.3_2562" {
		t.Errorf(`PackageLabel(real .dmg with dots in its own version) = %q, want "XeroxDrivers_5.19.3_2562" - a second blind Ext-based strip would mangle this`, got)
	}
}
