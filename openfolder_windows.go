package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OpenDriversBasePathInExplorer scaffolds path (see ensureDriversScaffold)
// if it's empty or doesn't exist yet, then opens it in File Explorer - the
// toolbar's own Drivers-folder button. path is resolved against this exe's
// current location first (see resolveExeRelative) in case it's the relative
// ".\Drivers" a portable copy's DriversBasePath now defaults to, rather than
// passing a relative path straight to os.MkdirAll/explorer.exe and hoping
// the process's own current working directory happens to line up.
func (a *App) OpenDriversBasePathInExplorer(path string) OpenFolderResult {
	path = resolveExeRelative(strings.TrimSpace(path))
	if path == "" {
		return OpenFolderResult{Error: "no folder path given"}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return OpenFolderResult{Error: fmt.Sprintf("creating %q: %v", path, err)}
	}
	if err := ensureDriversScaffold(path); err != nil {
		return OpenFolderResult{Error: err.Error()}
	}
	if err := exec.Command("explorer.exe", path).Start(); err != nil {
		return OpenFolderResult{Error: err.Error()}
	}
	return OpenFolderResult{}
}
