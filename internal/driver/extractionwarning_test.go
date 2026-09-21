package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureZipInfsExtracted_ReportsFailureViaExtractionWarning guards the
// real, previously invisible gap this exists to fix: a real local archive
// that BuildCatalog finds but can't actually extract an .inf from (Lexmark's
// own real package, confirmed live 2026-09-21 - see ExtractionWarning's own
// doc comment) used to fail with zero visible sign anywhere. A deliberately
// invalid .zip is enough to prove the wiring without needing a real broken
// archive fixture - archive/zip's own error is what warnExtraction reports.
func TestEnsureZipInfsExtracted_ReportsFailureViaExtractionWarning(t *testing.T) {
	prev := ExtractionWarning
	defer func() { ExtractionWarning = prev }()

	var got []string
	ExtractionWarning = func(msg string) { got = append(got, msg) }

	dir := t.TempDir()
	mfgDir := filepath.Join(dir, "Canon")
	if err := os.MkdirAll(mfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mfgDir, "Broken.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	ensureZipInfsExtracted(mfgDir)

	if len(got) != 1 {
		t.Fatalf("expected exactly one warning, got %v", got)
	}
	if !strings.Contains(got[0], "Canon") || !strings.Contains(got[0], "Broken.zip") {
		t.Errorf("expected the warning to name the manufacturer folder and the file, got %q", got[0])
	}
}

// TestWarnExtraction_NilHookIsANoOp guards the zero-value-means-off
// convention every real call site relies on (tests, and any build that
// never wires ExtractionWarning up, must never panic).
func TestWarnExtraction_NilHookIsANoOp(t *testing.T) {
	prev := ExtractionWarning
	defer func() { ExtractionWarning = prev }()
	ExtractionWarning = nil

	warnExtraction("this should be a no-op: %s", "fine")
}
