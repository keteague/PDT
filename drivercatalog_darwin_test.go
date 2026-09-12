package main

import (
	"testing"

	"PDT/internal/driver"
)

// readyApp builds an *App with ready already closed (so bound methods'
// own <-a.ready doesn't block) and the given macModelIndex/macCatalog
// pre-populated directly - these are unexported App fields, reachable here
// only because this test file lives in the same package.
func readyApp(modelIndex driver.MacModelIndex, catalog driver.MacCatalog) *App {
	a := &App{ready: make(chan struct{}), macModelIndex: modelIndex, macCatalog: catalog}
	close(a.ready)
	return a
}

func TestDefaultDriverFor_BlankForManufacturerWithModelIndex(t *testing.T) {
	a := readyApp(driver.MacModelIndex{
		"Canon": {"Some Model": nil},
	}, driver.MacCatalog{})
	if got := a.DefaultDriverFor("Canon"); got != "" {
		t.Errorf(`DefaultDriverFor("Canon") = %q, want "" (real per-model data - no single correct guess)`, got)
	}
}

func TestDefaultDriverFor_NoModelIndexEntryUnaffected(t *testing.T) {
	// Kyocera/Ricoh/Sharp etc. have no macFamilyPreference table at all, so
	// no model index entry - DefaultDriverFor should fall through to its
	// pre-existing ResolveMac-based guess (here: no local package either,
	// so "").
	a := readyApp(driver.MacModelIndex{}, driver.MacCatalog{Packages: map[string][]driver.MacPackage{}})
	if got := a.DefaultDriverFor("Kyocera"); got != "" {
		t.Errorf(`DefaultDriverFor("Kyocera") = %q, want "" (no local package, no model index entry)`, got)
	}
}

func TestMacModelManufacturers_SortedKeysOfModelIndex(t *testing.T) {
	a := readyApp(driver.MacModelIndex{
		"Xerox": {"X": nil},
		"Canon": {"C": nil},
	}, driver.MacCatalog{})
	got := a.MacModelManufacturers()
	want := []string{"Canon", "Xerox"}
	if len(got) != len(want) {
		t.Fatalf("MacModelManufacturers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MacModelManufacturers()[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}
