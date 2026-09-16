package cloudsync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"PDT/internal/driver"
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

// TestListLocal_SkipsExtractedSiblingFolder is listLocal's own integration
// test for GitHub issue #10's Sync-side fix: an archive's own extracted
// sibling folder (see driver.ExtractedSiblingDirs) must never be uploaded -
// it's a derived, re-creatable artifact of the archive sitting right next
// to it, and Cloud Sync carrying it too is exactly what made a real Drivers
// folder 5.4GB/22,570 files instead of ~1.5GB/~25.
func TestListLocal_SkipsExtractedSiblingFolder(t *testing.T) {
	root := t.TempDir()
	canon := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canon, "Driver.zip"), []byte("not a real zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	extractedDir := filepath.Join(canon, "Driver")
	if err := os.MkdirAll(extractedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractedDir, "driver.inf"), []byte("extracted content"), 0o644); err != nil {
		t.Fatal(err)
	}

	sizes := listLocal(root)
	if _, ok := sizes["Canon/Driver.zip"]; !ok {
		t.Errorf("expected the archive itself to still be listed, got %v", sizes)
	}
	if _, ok := sizes["Canon/Driver/driver.inf"]; ok {
		t.Errorf("expected Driver/ (the archive's extracted sibling) to be skipped entirely, got %v", sizes)
	}
}

// TestListLocal_SkipsPdtInfCacheFolder guards the other half of issue #10's
// skip rule - the .inf-only metadata cache the catalog rework writes into is
// never synced either.
func TestListLocal_SkipsPdtInfCacheFolder(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "Canon", driver.PdtInfCacheDirName, "Driver")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "driver.inf"), []byte("cached inf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Canon", "real.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	sizes := listLocal(root)
	if _, ok := sizes["Canon/real.txt"]; !ok {
		t.Errorf("expected the unrelated real file to still be listed, got %v", sizes)
	}
	for rel := range sizes {
		if filepath.Base(filepath.Dir(rel)) == driver.PdtInfCacheDirName || rel == "Canon/"+driver.PdtInfCacheDirName {
			t.Errorf("expected nothing under %s to be listed, got %q", driver.PdtInfCacheDirName, rel)
		}
	}
}

// TestListLocal_SkipsDSStore guards Finder's own per-folder metadata
// clutter (macOS creates a .DS_Store in nearly every folder it browses,
// including a Drivers folder synced from/to a Windows machine) - never real
// driver content, so Sync must never transfer it.
func TestListLocal_SkipsDSStore(t *testing.T) {
	root := t.TempDir()
	canon := filepath.Join(root, "Canon")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canon, driver.DSStoreFileName), []byte("finder metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canon, "real.txt"), []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	sizes := listLocal(root)
	if _, ok := sizes["Canon/real.txt"]; !ok {
		t.Errorf("expected the unrelated real file to still be listed, got %v", sizes)
	}
	if _, ok := sizes["Canon/"+driver.DSStoreFileName]; ok {
		t.Errorf("expected %s to be skipped entirely, got %v", driver.DSStoreFileName, sizes)
	}
}

// TestListRemote_SkipsDSStore guards the remote side of the same rule - a
// .DS_Store already sitting in the bucket from before this exclusion
// existed must disappear from the plan entirely, not show as a spurious
// "Download" now that the local side never lists one either. Uses a fake
// S3 ListObjectsV2 endpoint (same pattern as cancel_hang_listing_test.go's
// pagingForeverServer) since listRemote's own filtering happens while
// parsing that response, not in diff().
func TestListRemote_SkipsDSStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>test-bucket</Name>
  <Prefix>Drivers/</Prefix>
  <IsTruncated>false</IsTruncated>
  <Contents><Key>Drivers/Canon/real.txt</Key><Size>10</Size></Contents>
  <Contents><Key>Drivers/Canon/.DS_Store</Key><Size>4096</Size></Contents>
  <Contents><Key>Drivers/.DS_Store</Key><Size>4096</Size></Contents>
</ListBucketResult>`)
	}))
	defer srv.Close()
	core := testCore(t, srv)

	remote, err := listRemote(context.Background(), core, "test-bucket", "Drivers/")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := remote["Canon/real.txt"]; !ok {
		t.Errorf("expected the unrelated real file to still be listed, got %v", remote)
	}
	if _, ok := remote["Canon/"+driver.DSStoreFileName]; ok {
		t.Errorf("expected Canon/%s to be skipped, got %v", driver.DSStoreFileName, remote)
	}
	if _, ok := remote[driver.DSStoreFileName]; ok {
		t.Errorf("expected top-level %s to be skipped, got %v", driver.DSStoreFileName, remote)
	}
}
