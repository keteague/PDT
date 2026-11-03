package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// cloudSyncState is this technician's own per-machine bookkeeping for the
// Cloud Sync tree - separate from Settings (user-edited preferences) since
// this is bookkeeping the sync engine itself maintains rather than anything
// a person fills in through a form.
type cloudSyncState struct {
	// Deselected: relPath -> true means this technician explicitly
	// unchecked it in the tree view. Anything NOT listed here defaults to
	// selected - Ken's own reasoning for Cloud Sync generally ("we can all
	// benefit from new drivers") means a brand-new file nobody has ever
	// excluded should sync by default, not require an opt-in click first.
	Deselected map[string]bool `json:"deselected"`
	// Seen: relPath -> true once the tree view has shown it to this
	// technician at least once. GetCloudSyncPlan highlights anything NOT
	// in here as new, then folds every relPath it just returned into this
	// set - so the highlight shows exactly once, the very next time the
	// tree is opened after that path first appeared.
	Seen map[string]bool `json:"seen"`
	// InCloud: relPath -> true once this machine has seen it in the shared
	// bucket's listing. It's how a read-only technician's local-only file
	// is told apart (Ken, 2026-11-03): one that WAS in the cloud and has
	// since been removed there - offered for deletion as "Removed from
	// cloud" - versus one this technician got some other way (a
	// manufacturer download, a flash drive) that was simply never uploaded,
	// which is never offered. Only knows what it has seen, so a file
	// removed from the bucket before this field existed isn't flagged.
	// Entries for paths gone from both sides are pruned.
	InCloud map[string]bool `json:"inCloud"`
	// LocalOnly: relPath -> true once this technician chose "Keep local
	// only" for a removed-from-cloud file - it's kept on this computer,
	// never synced, and no longer asked about (it moves under "Hide files
	// already in sync"). Pruned once the path stops being a removed file.
	LocalOnly map[string]bool `json:"localOnly"`
}

func cloudSyncStateFilePath() (string, error) {
	return appStateFilePath("cloudsync-state.json")
}

// loadCloudSyncState reads the persisted state, falling back to an empty
// (everything selected, nothing yet seen) one for a missing or corrupt file
// - this is bookkeeping, not user data worth failing startup over.
func loadCloudSyncState() cloudSyncState {
	s := cloudSyncState{Deselected: map[string]bool{}, Seen: map[string]bool{}, InCloud: map[string]bool{}, LocalOnly: map[string]bool{}}
	path, err := cloudSyncStateFilePath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var loaded cloudSyncState
	if err := json.Unmarshal(data, &loaded); err != nil {
		return s
	}
	if loaded.Deselected != nil {
		s.Deselected = loaded.Deselected
	}
	if loaded.Seen != nil {
		s.Seen = loaded.Seen
	}
	if loaded.InCloud != nil {
		s.InCloud = loaded.InCloud
	}
	if loaded.LocalOnly != nil {
		s.LocalOnly = loaded.LocalOnly
	}
	return s
}

func saveCloudSyncState(s cloudSyncState) error {
	path, err := cloudSyncStateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
