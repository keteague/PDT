package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestNearestExistingDir is the direct regression test for a real bug
// reported live: Settings > General's Browse ("...") button silently did
// nothing at all for Configuration Files Base Path and Preinstall Base
// Path (but not Drivers Base Path) - because whichever of those was
// currently set to a folder that doesn't exist yet (nothing scaffolds
// Documents\Preinstalls the way Drivers/Configs get bootstrapped) made
// Wails' own runtime.OpenDirectoryDialog refuse to even show the dialog,
// and the frontend's click handler has no .catch() to surface that
// rejected promise.
func TestNearestExistingDir(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Documents")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(existing, "Preinstalls")

	if got := nearestExistingDir(missing); got != existing {
		t.Errorf("nearestExistingDir(%q) = %q, want %q", missing, got, existing)
	}
	if got := nearestExistingDir(existing); got != existing {
		t.Errorf("nearestExistingDir(%q) = %q, want %q (already exists)", existing, got, existing)
	}
	if got := nearestExistingDir(""); got != "" {
		t.Errorf(`nearestExistingDir("") = %q, want ""`, got)
	}
	if got := nearestExistingDir(filepath.Join(root, "NoSuchRootAtAll", "Nested")); got != root {
		t.Errorf("nearestExistingDir of a path with no existing ancestor but root = %q, want %q", got, root)
	}
}

func TestApplyManufacturerOrder(t *testing.T) {
	items := []string{"Canon", "HP", "Sharp"}
	order := []string{"Sharp", "Canon", "HP"}

	got := applyManufacturerOrder(items, order)
	want := []string{"Sharp", "Canon", "HP"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("applyManufacturerOrder(%v, %v) = %v, want %v", items, order, got, want)
	}
}

// TestApplyManufacturerOrder_UnlistedItemAppendedAtEnd covers a manufacturer
// that has drivers present (so it's in items) but isn't in the user's saved
// order yet (e.g. its drivers were only just added) - it should still show
// up, appended after everything the user has actually ordered, rather than
// being dropped.
func TestApplyManufacturerOrder_UnlistedItemAppendedAtEnd(t *testing.T) {
	items := []string{"Canon", "HP", "Xerox"}
	order := []string{"HP", "Canon"}

	got := applyManufacturerOrder(items, order)
	want := []string{"HP", "Canon", "Xerox"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("applyManufacturerOrder(%v, %v) = %v, want %v", items, order, got, want)
	}
}
