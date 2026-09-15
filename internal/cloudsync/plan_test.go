package cloudsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiff_ClassifiesEveryCase(t *testing.T) {
	local := map[string]int64{
		"upload-only.zip": 100,
		"same-size.zip":   200,
		"mismatch.zip":    300,
	}
	remote := map[string]remoteObject{
		"same-size.zip":     {size: 200},
		"mismatch.zip":      {size: 999},
		"download-only.zip": {size: 400},
	}

	items := diff(local, remote)
	got := map[string]Action{}
	for _, it := range items {
		got[it.RelPath] = it.Action
	}

	want := map[string]Action{
		"upload-only.zip":   ActionUpload,
		"download-only.zip": ActionDownload,
		"same-size.zip":     ActionSynced,
		"mismatch.zip":      ActionConflict,
	}
	for rel, wantAction := range want {
		if got[rel] != wantAction {
			t.Errorf("diff()[%q] = %q, want %q", rel, got[rel], wantAction)
		}
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(items), len(want), items)
	}
}

func TestDiff_ConflictNeverPropagatesEitherDirection(t *testing.T) {
	// The one case diff refuses to guess about - confirms it's classified
	// as its own distinct Action rather than silently folded into upload or
	// download, which would mean Sync overwrites one side without anyone
	// having decided that's safe.
	items := diff(map[string]int64{"f.zip": 100}, map[string]remoteObject{"f.zip": {size: 200}})
	if len(items) != 1 || items[0].Action != ActionConflict {
		t.Fatalf("expected a single ActionConflict item, got %+v", items)
	}
	if items[0].LocalSize != 100 || items[0].RemoteSize != 200 {
		t.Errorf("expected both sizes preserved for inspection, got local=%d remote=%d", items[0].LocalSize, items[0].RemoteSize)
	}
}

func TestListLocal_EmptyForMissingRoot(t *testing.T) {
	sizes := listLocal(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(sizes) != 0 {
		t.Errorf("expected an empty map for a nonexistent root, got %v", sizes)
	}
}

func TestListLocal_UsesForwardSlashRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Canon", "26-Tahoe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Canon", "26-Tahoe", "driver.pkg"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	sizes := listLocal(root)
	if sizes["Canon/26-Tahoe/driver.pkg"] != 5 {
		t.Errorf("got %v, want a 5-byte entry at \"Canon/26-Tahoe/driver.pkg\" (forward slashes even on Windows)", sizes)
	}
}
