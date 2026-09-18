package main

import (
	"os"
	"path/filepath"
)

// userDocumentsDir is ~/Documents - macOS has no relocatable Documents
// equivalent to Windows' folder redirection (iCloud Drive's "Desktop &
// Documents" sync still surfaces it at this same path).
func userDocumentsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Documents")
	}
	return ""
}
