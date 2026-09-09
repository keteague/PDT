package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OpenDriversBasePathInExplorer creates path if it doesn't exist yet, then
// opens it in Finder - the toolbar's own Drivers-folder button. Unlike the
// Windows version, this doesn't call ensureDriversScaffold: that scaffolds
// the Windows-shaped Drivers\Windows\11\<Manufacturer>\Archive tree, which
// has no place inside a macOS Drivers/macOS/... folder (there is no macOS
// scaffold built yet - see app_darwin.go's platformStartup).
func (a *App) OpenDriversBasePathInExplorer(path string) OpenFolderResult {
	path = resolveExeRelative(strings.TrimSpace(path))
	if path == "" {
		return OpenFolderResult{Error: "no folder path given"}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return OpenFolderResult{Error: fmt.Sprintf("creating %q: %v", path, err)}
	}
	if err := exec.Command("open", path).Start(); err != nil {
		return OpenFolderResult{Error: err.Error()}
	}
	return OpenFolderResult{}
}
