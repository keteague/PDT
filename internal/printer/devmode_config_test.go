package printer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilenamePart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Copy Room", "Copy Room"},
		{`Front Desk\2`, "Front Desk2"},
		{"3rd Floor: Reception", "3rd Floor Reception"},
		{`<Weird*Name?>`, "WeirdName"},
	}
	for _, c := range cases {
		if got := SanitizeFilenamePart(c.in); got != c.want {
			t.Errorf("SanitizeFilenamePart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDevModeFileName(t *testing.T) {
	if got, want := DevModeFileName("18455-1", "Copy Room"), "18455-1-Copy Room.bin"; got != want {
		t.Errorf("DevModeFileName = %q, want %q", got, want)
	}
}

func TestResolveDevModePath_ExplicitPointer(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit-name.bin")
	if err := os.WriteFile(explicit, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	row := PrinterRow{Name: "Copy Room", DevModeFile: "explicit-name.bin"}
	path, ok := ResolveDevModePath(dir, "18455-1", row)
	if !ok {
		t.Fatal("expected the explicit pointer to resolve")
	}
	if path != explicit {
		t.Errorf("ResolveDevModePath = %q, want %q", path, explicit)
	}
}

func TestResolveDevModePath_FallsBackToConventionalName(t *testing.T) {
	dir := t.TempDir()
	conventional := filepath.Join(dir, "18455-1-Copy Room.bin")
	if err := os.WriteFile(conventional, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Row has no explicit pointer (e.g. a fresh run after forgetting to save
	// the JSON) - should still find the file the earlier capture wrote under
	// the conventional name.
	row := PrinterRow{Name: "Copy Room"}
	path, ok := ResolveDevModePath(dir, "18455-1", row)
	if !ok {
		t.Fatal("expected the conventional-name fallback to resolve")
	}
	if path != conventional {
		t.Errorf("ResolveDevModePath = %q, want %q", path, conventional)
	}
}

func TestResolveDevModePath_ExplicitPointerMissingFallsBackToConventional(t *testing.T) {
	dir := t.TempDir()
	conventional := filepath.Join(dir, "18455-1-Copy Room.bin")
	if err := os.WriteFile(conventional, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The row's own pointer references a file that no longer exists - should
	// still recover via the conventional name rather than giving up.
	row := PrinterRow{Name: "Copy Room", DevModeFile: "does-not-exist.bin"}
	path, ok := ResolveDevModePath(dir, "18455-1", row)
	if !ok {
		t.Fatal("expected fallback to the conventional name when the explicit pointer is missing")
	}
	if path != conventional {
		t.Errorf("ResolveDevModePath = %q, want %q", path, conventional)
	}
}

func TestResolveDevModePath_NotFound(t *testing.T) {
	dir := t.TempDir()
	row := PrinterRow{Name: "Copy Room"}
	if _, ok := ResolveDevModePath(dir, "18455-1", row); ok {
		t.Error("expected no match when neither the pointer nor the conventional name exist")
	}
}

func TestDriverDataFileName(t *testing.T) {
	if got, want := DriverDataFileName("18455-1", "Copy Room"), "18455-1-Copy Room.driverdata.json"; got != want {
		t.Errorf("DriverDataFileName = %q, want %q", got, want)
	}
}

func TestResolveDriverDataPath_DerivedFromDevModeFile(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "explicit-name.driverdata.json")
	if err := os.WriteFile(sidecar, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	row := PrinterRow{Name: "Copy Room", DevModeFile: "explicit-name.bin"}
	path, ok := ResolveDriverDataPath(dir, "18455-1", row)
	if !ok {
		t.Fatal("expected the sidecar derived from DevModeFile's basename to resolve")
	}
	if path != sidecar {
		t.Errorf("ResolveDriverDataPath = %q, want %q", path, sidecar)
	}
}

func TestResolveDriverDataPath_FallsBackToConventionalName(t *testing.T) {
	dir := t.TempDir()
	conventional := filepath.Join(dir, "18455-1-Copy Room.driverdata.json")
	if err := os.WriteFile(conventional, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	row := PrinterRow{Name: "Copy Room"}
	path, ok := ResolveDriverDataPath(dir, "18455-1", row)
	if !ok {
		t.Fatal("expected the conventional-name fallback to resolve")
	}
	if path != conventional {
		t.Errorf("ResolveDriverDataPath = %q, want %q", path, conventional)
	}
}

func TestResolveDriverDataPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	// A DEVMODE with no driver-data sidecar at all is routine (see the
	// function's own comment) - just no match, not an error.
	row := PrinterRow{Name: "Copy Room", DevModeFile: "18455-1-Copy Room.bin"}
	if _, ok := ResolveDriverDataPath(dir, "18455-1", row); ok {
		t.Error("expected no match when no sidecar file exists")
	}
}
