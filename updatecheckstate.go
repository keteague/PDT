package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// updateCheckState is when each automatic update check last completed
// successfully - bookkeeping the app maintains itself, so it lives beside
// settings.json rather than in it (see cloudSyncState for the same split):
// Settings is round-tripped whole through the frontend on every save, which
// could otherwise overwrite a timestamp the backend just recorded.
type updateCheckState struct {
	PDT      time.Time `json:"pdt"`
	SevenZip time.Time `json:"sevenZip"`
}

func updateCheckStateFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PDT", "update-check-state.json"), nil
}

// loadUpdateCheckState reads the persisted state, falling back to "never
// checked" (both zero) for a missing or corrupt file - the worst outcome is
// one extra check, not worth failing over.
func loadUpdateCheckState() updateCheckState {
	path, err := updateCheckStateFilePath()
	if err != nil {
		return updateCheckState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateCheckState{}
	}
	var st updateCheckState
	if err := json.Unmarshal(data, &st); err != nil {
		return updateCheckState{}
	}
	return st
}

func saveUpdateCheckState(st updateCheckState) error {
	path, err := updateCheckStateFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
