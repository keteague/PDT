package main

import (
	"reflect"
	"testing"

	"PDT/internal/driver"
)

func TestReconcileManufacturerOrder_Nil(t *testing.T) {
	got := reconcileManufacturerOrder(nil)
	if !reflect.DeepEqual(got, driver.Manufacturers) {
		t.Errorf("reconcileManufacturerOrder(nil) = %v, want %v", got, driver.Manufacturers)
	}
}

func TestReconcileManufacturerOrder_PreservesCustomOrder(t *testing.T) {
	saved := []string{"Sharp", "Canon"}
	got := reconcileManufacturerOrder(saved)

	if got[0] != "Sharp" || got[1] != "Canon" {
		t.Fatalf("expected Sharp then Canon first, got %v", got)
	}
	if len(got) != len(driver.Manufacturers) {
		t.Fatalf("expected every manufacturer present exactly once, got %d of %d: %v", len(got), len(driver.Manufacturers), got)
	}
	seen := map[string]int{}
	for _, m := range got {
		seen[m]++
	}
	for _, m := range driver.Manufacturers {
		if seen[m] != 1 {
			t.Errorf("%q appears %d times in reconciled order, want exactly 1", m, seen[m])
		}
	}
}

func TestReconcileManufacturerOrder_DropsUnknownAndDuplicates(t *testing.T) {
	saved := []string{"Canon", "Canon", "NotARealManufacturer", "HP"}
	got := reconcileManufacturerOrder(saved)

	if got[0] != "Canon" || got[1] != "HP" {
		t.Fatalf("expected Canon then HP first (duplicate/unknown entries dropped), got %v", got)
	}
	for _, m := range got {
		if m == "NotARealManufacturer" {
			t.Error("unknown manufacturer name should have been dropped")
		}
	}
}
