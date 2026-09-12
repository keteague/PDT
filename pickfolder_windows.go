package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

// pickFolderDialog is PickFolder's (app.go) platform hook on Windows - just
// Wails' own Common-File-Dialog-backed runtime.OpenDirectoryDialog, unchanged
// from before this was split out (macOS needs a different implementation -
// see pickfolder_darwin.go's own doc comment for why).
func (a *App) pickFolderDialog(title, defaultDir string) (path string, canceled bool, err error) {
	path, err = runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                title,
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
	return path, err == nil && path == "", err
}
