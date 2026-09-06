package main

import (
	"reflect"
	"testing"
)

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
