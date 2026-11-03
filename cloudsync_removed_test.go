package main

import (
	"os"
	"path/filepath"
	"testing"

	"PDT/internal/cloudsync"
)

func newTestCloudSyncState() cloudSyncState {
	return cloudSyncState{Deselected: map[string]bool{}, Seen: map[string]bool{}, InCloud: map[string]bool{}, LocalOnly: map[string]bool{}}
}

func itemByPath(items []CloudSyncPlanItem, rel string) (CloudSyncPlanItem, bool) {
	for _, it := range items {
		if it.RelPath == rel {
			return it, true
		}
	}
	return CloudSyncPlanItem{}, false
}

// A read-only technician sees a local-only file as "removed from cloud"
// (unchecked) only if this machine has seen it in the cloud before; a
// local-only file that was never there stays hidden.
func TestPlanTreeItems_ReadOnlyRemovedFromCloud(t *testing.T) {
	state := newTestCloudSyncState()

	// First sync: the old driver is still in the cloud.
	planTreeItems([]cloudsync.PlanItem{
		{RelPath: "Kyocera/KXDriver_8.6.zip", Action: cloudsync.ActionSynced, LocalSize: 10, RemoteSize: 10},
		{RelPath: "Kyocera/MyOwnDownload.zip", Action: cloudsync.ActionUpload, LocalSize: 5},
	}, &state, true)

	// Later: the cloud has dropped it; this laptop still has it.
	items := planTreeItems([]cloudsync.PlanItem{
		{RelPath: "Kyocera/KXDriver_8.6.zip", Action: cloudsync.ActionUpload, LocalSize: 10},
		{RelPath: "Kyocera/MyOwnDownload.zip", Action: cloudsync.ActionUpload, LocalSize: 5},
		{RelPath: "Kyocera/KXDriver_8.7.zip", Action: cloudsync.ActionDownload, RemoteSize: 12},
	}, &state, true)

	old, ok := itemByPath(items, "Kyocera/KXDriver_8.6.zip")
	if !ok || old.Action != cloudSyncActionRemoved || old.Selected {
		t.Errorf("expected the old driver as an unchecked removed row, got %+v (present=%v)", old, ok)
	}
	if _, ok := itemByPath(items, "Kyocera/MyOwnDownload.zip"); ok {
		t.Error("a file that was never in the cloud must not be offered for deletion")
	}
	if dl, ok := itemByPath(items, "Kyocera/KXDriver_8.7.zip"); !ok || dl.Action != string(cloudsync.ActionDownload) {
		t.Errorf("expected the new driver as a download, got %+v", dl)
	}
}

// A technician with write access gets a removed row too (with the Sync
// choice in the UI), while a local-only file never in the cloud is still an
// ordinary upload for them.
func TestPlanTreeItems_WriterSeesRemovedAndUploads(t *testing.T) {
	state := newTestCloudSyncState()
	state.InCloud["Kyocera/KXDriver_8.6.zip"] = true

	items := planTreeItems([]cloudsync.PlanItem{
		{RelPath: "Kyocera/KXDriver_8.6.zip", Action: cloudsync.ActionUpload, LocalSize: 10},
		{RelPath: "Kyocera/Brand_new.zip", Action: cloudsync.ActionUpload, LocalSize: 4},
	}, &state, false)

	if it, _ := itemByPath(items, "Kyocera/KXDriver_8.6.zip"); it.Action != cloudSyncActionRemoved || it.Selected {
		t.Errorf("expected an unchecked removed row for a writer, got %+v", it)
	}
	if it, _ := itemByPath(items, "Kyocera/Brand_new.zip"); it.Action != string(cloudsync.ActionUpload) {
		t.Errorf("expected an upload for a never-in-cloud file, got %+v", it)
	}
}

// "Keep local only" is reported on the row, and dropped once the file is
// back in the cloud.
func TestPlanTreeItems_LocalOnly(t *testing.T) {
	state := newTestCloudSyncState()
	state.InCloud["Kyocera/old.zip"] = true
	state.LocalOnly["Kyocera/old.zip"] = true

	items := planTreeItems([]cloudsync.PlanItem{
		{RelPath: "Kyocera/old.zip", Action: cloudsync.ActionUpload, LocalSize: 10},
	}, &state, false)
	if it, _ := itemByPath(items, "Kyocera/old.zip"); !it.LocalOnly {
		t.Errorf("expected LocalOnly on the row, got %+v", it)
	}

	planTreeItems([]cloudsync.PlanItem{
		{RelPath: "Kyocera/old.zip", Action: cloudsync.ActionSynced, LocalSize: 10, RemoteSize: 10},
	}, &state, false)
	if state.LocalOnly["Kyocera/old.zip"] {
		t.Error("LocalOnly should be dropped once the file is back in the cloud")
	}
}

func TestDecideRemoved(t *testing.T) {
	cases := []struct {
		choice              string
		localOnly, readOnly bool
		want                int
	}{
		{"", false, false, removedOutcomeNone},
		{"delete", false, false, removedOutcomeDelete},
		{"delete", false, true, removedOutcomeDelete},
		{"sync", false, false, removedOutcomeUpload},
		{"sync", false, true, removedOutcomeSkippedUpload},
		{"delete", true, false, removedOutcomeNone},
		{"sync", true, false, removedOutcomeNone},
		{"bogus", false, false, removedOutcomeNone},
	}
	for _, c := range cases {
		if got := decideRemoved(c.choice, c.localOnly, c.readOnly); got != c.want {
			t.Errorf("decideRemoved(%q, localOnly=%v, readOnly=%v) = %d, want %d", c.choice, c.localOnly, c.readOnly, got, c.want)
		}
	}
}

// InCloud only keeps paths still on at least one side.
func TestRecordInCloud_PrunesPathsGoneEverywhere(t *testing.T) {
	state := newTestCloudSyncState()
	state.InCloud["gone/everywhere.zip"] = true
	recordInCloud(&state, []cloudsync.PlanItem{{RelPath: "still/here.zip", Action: cloudsync.ActionSynced}})
	if state.InCloud["gone/everywhere.zip"] || !state.InCloud["still/here.zip"] {
		t.Errorf("unexpected InCloud: %v", state.InCloud)
	}
}

func TestDeleteRemovedLocal(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Kyocera", "old.zip")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.zip")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	if err := deleteRemovedLocal(root, "Kyocera/old.zip"); err != nil {
		t.Fatal(err)
	}
	if fileExists(target) {
		t.Error("expected the file deleted")
	}
	if err := deleteRemovedLocal(root, "../outside.zip"); err == nil || !fileExists(outside) {
		t.Error("a path outside the Drivers folder must be refused")
	}
	if err := deleteRemovedLocal(root, "Kyocera/already-gone.zip"); err != nil {
		t.Errorf("an already-missing file isn't an error, got %v", err)
	}
}
