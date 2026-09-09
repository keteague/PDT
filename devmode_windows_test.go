package main

import (
	"testing"

	"PDT/internal/driver"
)

func TestIsPhysicalPrinterGuess(t *testing.T) {
	physical := []string{
		"Canon Generic Plus UFR II",
		"HP Universal Printing PCL 6",
		"RICOH PCL6 UniversalDriver V4.45",
	}
	for _, d := range physical {
		if !isPhysicalPrinterGuess(d) {
			t.Errorf("isPhysicalPrinterGuess(%q) = false, want true", d)
		}
	}

	virtual := []string{
		"Microsoft Print To PDF",
		"Microsoft XPS Document Writer",
		"Microsoft Shared Fax Driver",
		"Send to Microsoft OneNote 2016 Driver",
	}
	for _, d := range virtual {
		if isPhysicalPrinterGuess(d) {
			t.Errorf("isPhysicalPrinterGuess(%q) = true, want false", d)
		}
	}
}

func TestFindManufacturerForDriver(t *testing.T) {
	cat := driver.Catalog{
		"Canon": {"Canon Generic Plus UFR II": nil},
		"HP":    {"HP Universal Printing PCL 6": nil},
	}
	if got, want := findManufacturerForDriver(cat, "Canon Generic Plus UFR II"), "Canon"; got != want {
		t.Errorf("findManufacturerForDriver = %q, want %q", got, want)
	}
	if got := findManufacturerForDriver(cat, "Some Unknown Driver"); got != "" {
		t.Errorf("findManufacturerForDriver(unknown) = %q, want \"\"", got)
	}
}
