package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// TestDriversRoot_FallsBackWhenConfiguredPathDoesNotExist is the direct
// regression test for a real bug reported live (2026-09-29): Settings had
// DriversBasePath saved as an absolute "E:\Drivers" from an earlier session;
// on two different target PCs the flash drive mounted under a different
// letter (F: and D:), but PDT kept scanning the stale, nonexistent E: path
// with no indication anything was wrong. driversRoot() must fall back to a
// freshly recomputed default (which resolves against wherever the exe
// actually is *this run* - see resolveExeRelative) whenever the configured
// value doesn't exist, rather than trusting it forever.
func TestDriversRoot_FallsBackWhenConfiguredPathDoesNotExist(t *testing.T) {
	oldPath, oldNotice := currentDriversBasePath, BasePathFallbackNotice
	defer func() { currentDriversBasePath, BasePathFallbackNotice = oldPath, oldNotice }()

	staleAbsolute := filepath.Join(t.TempDir(), "NoLongerAttached", "Drivers")
	currentDriversBasePath = staleAbsolute

	var notices []string
	BasePathFallbackNotice = func(msg string) { notices = append(notices, msg) }

	want := resolveExeRelative(defaultDriversBasePath())
	if got := driversRoot(); got != want {
		t.Errorf("driversRoot() = %q, want freshly recomputed default %q", got, want)
	}
	if len(notices) != 1 {
		t.Fatalf("BasePathFallbackNotice called %d times, want 1", len(notices))
	}
	if !strings.Contains(notices[0], staleAbsolute) || !strings.Contains(notices[0], want) {
		t.Errorf("fallback notice = %q, want it to mention both %q and %q", notices[0], staleAbsolute, want)
	}
}

// TestDriversRoot_UsesConfiguredPathWhenItExists confirms the fallback above
// never kicks in for a legitimate configured path (e.g. an admin-picked
// network share on an installed copy) just because it happens to be
// absolute - only a configured path that's actually missing gets overridden.
func TestDriversRoot_UsesConfiguredPathWhenItExists(t *testing.T) {
	oldPath, oldNotice := currentDriversBasePath, BasePathFallbackNotice
	defer func() { currentDriversBasePath, BasePathFallbackNotice = oldPath, oldNotice }()

	real := t.TempDir()
	currentDriversBasePath = real

	noticed := false
	BasePathFallbackNotice = func(string) { noticed = true }

	if got := driversRoot(); got != real {
		t.Errorf("driversRoot() = %q, want unchanged configured path %q", got, real)
	}
	if noticed {
		t.Error("BasePathFallbackNotice fired for a configured path that actually exists")
	}
}

// TestConfigsRoot_FallsBackWhenConfiguredPathDoesNotExist is configsRoot's
// own sibling of TestDriversRoot_FallsBackWhenConfiguredPathDoesNotExist -
// same bug, same fix, same reasoning (see driversRoot's own doc comment).
func TestConfigsRoot_FallsBackWhenConfiguredPathDoesNotExist(t *testing.T) {
	oldPath, oldNotice := currentConfigsBasePath, BasePathFallbackNotice
	defer func() { currentConfigsBasePath, BasePathFallbackNotice = oldPath, oldNotice }()

	staleAbsolute := filepath.Join(t.TempDir(), "NoLongerAttached", "Configs")
	currentConfigsBasePath = staleAbsolute

	var notices []string
	BasePathFallbackNotice = func(msg string) { notices = append(notices, msg) }

	want := resolveExeRelative(defaultSaveFileBasePath())
	if got := configsRoot(); got != want {
		t.Errorf("configsRoot() = %q, want freshly recomputed default %q", got, want)
	}
	if len(notices) != 1 {
		t.Fatalf("BasePathFallbackNotice called %d times, want 1", len(notices))
	}
}

// TestCallFindFolderOnAnyDrive_NilIsNoOp confirms driversRoot/configsRoot's
// tier-3 fallback is a safe no-op (never panics, just contributes nothing)
// on darwin and in any test that hasn't wired findFolderOnAnyDrive up - the
// same zero-value-means-off convention BasePathFallbackNotice and
// driver.ExtractionWarning both already use.
func TestCallFindFolderOnAnyDrive_NilIsNoOp(t *testing.T) {
	old := findFolderOnAnyDrive
	defer func() { findFolderOnAnyDrive = old }()
	findFolderOnAnyDrive = nil

	if got := callFindFolderOnAnyDrive("Drivers"); got != "" {
		t.Errorf(`callFindFolderOnAnyDrive("Drivers") = %q, want "" with findFolderOnAnyDrive unset`, got)
	}
}

// TestCallFindFolderOnAnyDrive_DelegatesWhenSet confirms the folder name
// passed to driversRoot/configsRoot's tier-3 fallback reaches whatever's
// wired up (findFolderOnAnyRemovableDrive on Windows - see app_windows.go),
// and that fallback's return value passes straight back through -
// callFindFolderOnAnyDrive itself does no interpretation of either.
func TestCallFindFolderOnAnyDrive_DelegatesWhenSet(t *testing.T) {
	old := findFolderOnAnyDrive
	defer func() { findFolderOnAnyDrive = old }()

	var gotArg string
	findFolderOnAnyDrive = func(folderName string) string {
		gotArg = folderName
		return `X:\Somewhere\Drivers`
	}

	if got := callFindFolderOnAnyDrive("Drivers"); got != `X:\Somewhere\Drivers` {
		t.Errorf(`callFindFolderOnAnyDrive("Drivers") = %q, want the wired function's return value`, got)
	}
	if gotArg != "Drivers" {
		t.Errorf("wired function received folderName %q, want %q", gotArg, "Drivers")
	}
}

// TestConfigsRoot_UsesConfiguredPathWhenItExists is
// TestDriversRoot_UsesConfiguredPathWhenItExists's own sibling for configsRoot.
func TestConfigsRoot_UsesConfiguredPathWhenItExists(t *testing.T) {
	oldPath, oldNotice := currentConfigsBasePath, BasePathFallbackNotice
	defer func() { currentConfigsBasePath, BasePathFallbackNotice = oldPath, oldNotice }()

	real := t.TempDir()
	currentConfigsBasePath = real

	noticed := false
	BasePathFallbackNotice = func(string) { noticed = true }

	if got := configsRoot(); got != real {
		t.Errorf("configsRoot() = %q, want unchanged configured path %q", got, real)
	}
	if noticed {
		t.Error("BasePathFallbackNotice fired for a configured path that actually exists")
	}
}
