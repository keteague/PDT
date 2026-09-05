package driver

import (
	"runtime"
	"testing"
)

func TestResolve_PlainNameResolvesToNewestCompatible(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("this table assumes the dev/CI host is amd64")
	}
	cat := testCatalog(t)
	got, err := Resolve(cat, "Kyocera", "Kyocera FS-1100 KX")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil {
		t.Fatal("Resolve returned nil, want the newest compatible version")
	}
	if got.Version != "8.7.0422.0" {
		t.Errorf("Version = %q, want 8.7.0422.0 (newest)", got.Version)
	}
	if got.IsExplicitVersion {
		t.Error("IsExplicitVersion = true for a plain-name selection, want false")
	}
}

func TestResolve_DecoratedLabelPinsExactVersion(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("this table assumes the dev/CI host is amd64")
	}
	cat := testCatalog(t)
	got, err := Resolve(cat, "Kyocera", "Kyocera FS-1100 KX (v8.6.1022.0 - 2025-10-22, 64bit)")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil {
		t.Fatal("Resolve returned nil, want the pinned older version")
	}
	if got.Version != "8.6.1022.0" {
		t.Errorf("Version = %q, want 8.6.1022.0 (explicit pin)", got.Version)
	}
	if !got.IsExplicitVersion {
		t.Error("IsExplicitVersion = false for a decorated-label selection, want true")
	}
}

func TestResolve_StaleVersionFallsBackToNewest(t *testing.T) {
	cat := testCatalog(t)
	got, err := Resolve(cat, "Kyocera", "Kyocera FS-1100 KX (v9.9.9999.0 - 2099-01-01)")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil {
		t.Fatal("Resolve returned nil, want fallback to newest compatible version")
	}
	if got.IsExplicitVersion {
		t.Error("IsExplicitVersion = true after falling back from a not-found pin, want false")
	}
}

func TestResolve_UnknownDriverReturnsNil(t *testing.T) {
	cat := testCatalog(t)
	got, err := Resolve(cat, "Kyocera", "Not A Real Driver")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != nil {
		t.Errorf("Resolve() = %v, want nil for an unknown driver name", got)
	}
}
