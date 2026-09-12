package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonCoreDevicePackages_FindsBothByRealNamingConvention(t *testing.T) {
	dir := t.TempDir()
	// Real Canon UFR II naming, confirmed against a live download - a
	// version-varying prefix in front of the fixed "_Core.pkg"/"_Device.pkg"
	// suffix this matches on.
	for _, name := range []string{
		"Canon_Family_Printer_Core.pkg",
		"Canon_Family_Printer_Device.pkg",
		"Canon_Family_Printer_Icons.pkg",
		"Canon_Family_Printer_Profiles.pkg",
		"Canon_Family_Printer_cnaccm.pkg",
		"Resources",
	} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Distribution"), []byte("<xml/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	core, device, ok := CanonCoreDevicePackages(dir)
	if !ok {
		t.Fatal("CanonCoreDevicePackages ok = false, want true")
	}
	if want := filepath.Join(dir, "Canon_Family_Printer_Core.pkg"); core != want {
		t.Errorf("core = %q, want %q", core, want)
	}
	if want := filepath.Join(dir, "Canon_Family_Printer_Device.pkg"); device != want {
		t.Errorf("device = %q, want %q", device, want)
	}
}

func TestCanonCoreDevicePackages_MissingEitherSubPackageIsNotOK(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Canon_Family_Printer_Core.pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No "_Device.pkg" at all - an unexpected/different Distribution shape.
	if _, _, ok := CanonCoreDevicePackages(dir); ok {
		t.Error("ok = true with no Device sub-package present, want false")
	}
}

func TestCanonCoreDevicePackages_MissingDirReturnsNotOK(t *testing.T) {
	if _, _, ok := CanonCoreDevicePackages(filepath.Join(t.TempDir(), "does-not-exist")); ok {
		t.Error("ok = true for a nonexistent directory, want false")
	}
}

func TestCanonPPDBaseName_StripsRealExtensions(t *testing.T) {
	tests := map[string]string{
		"CNPZUIFC5150ZU.ppd.gz":  "CNPZUIFC5150ZU",
		"CNPZUIRAC5735ZU.ppd.gz": "CNPZUIRAC5735ZU",
		"CNPZUIFC5150ZU.ppd":     "CNPZUIFC5150ZU",
	}
	for in, want := range tests {
		if got := CanonPPDBaseName(in); got != want {
			t.Errorf("CanonPPDBaseName(%q) = %q, want %q", in, got, want)
		}
	}
}
