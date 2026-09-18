package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// userDocumentsDir is the user's real Documents folder - asked of the shell
// (FOLDERID_Documents) rather than assumed to be %UserProfile%\Documents,
// since OneDrive Known Folder Move (and plain folder redirection) relocates
// it, e.g. to "C:\Users\<user>\OneDrive - <Org>\Documents", leaving
// %UserProfile%\Documents nonexistent or empty. Falls back to the
// home-relative guess only if the shell can't answer.
func userDocumentsDir() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, windows.KF_FLAG_DEFAULT); err == nil && p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Documents")
	}
	return ""
}
