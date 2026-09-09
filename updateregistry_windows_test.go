package main

import (
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// TestSetRegistryDisplayVersion runs against a throwaway key under
// CURRENT_USER\SOFTWARE (always writable by the current user, no
// administrator rights needed) rather than this project's own real
// uninstall entry, so it's safe to run on any machine, including CI.
func TestSetRegistryDisplayVersion(t *testing.T) {
	path := `SOFTWARE\PDT-test-` + strconv.FormatInt(time.Now().UnixNano(), 36)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("DisplayVersion", "0.0.0"); err != nil {
		t.Fatal(err)
	}
	k.Close()
	defer registry.DeleteKey(registry.CURRENT_USER, path)

	setRegistryDisplayVersion(registry.CURRENT_USER, path, "9.9.9")

	k2, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k2.Close()
	val, _, err := k2.GetStringValue("DisplayVersion")
	if err != nil {
		t.Fatal(err)
	}
	if val != "9.9.9" {
		t.Errorf("DisplayVersion = %q, want %q", val, "9.9.9")
	}
}

// TestSetRegistryDisplayVersion_NoOpForMissingKey guards the exact case a
// portable/flash-drive copy hits every time - never installed via the Inno
// Setup installer at all, so this key never exists in either hive - which
// must be a silent no-op, not a panic or error.
func TestSetRegistryDisplayVersion_NoOpForMissingKey(t *testing.T) {
	setRegistryDisplayVersion(registry.CURRENT_USER, `SOFTWARE\PDT-test-key-that-does-not-exist-12345`, "9.9.9")
}
