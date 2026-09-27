package main

// pickFolderDialog is PickFolder's (app.go) platform hook on Windows - just
// Wails' own Common-File-Dialog-backed directory dialog (uiRuntime's
// PickDirectory), unchanged from before this was split out (macOS needs a
// different implementation - see pickfolder_darwin.go's own doc comment for
// why).
func (a *App) pickFolderDialog(title, defaultDir string) (path string, canceled bool, err error) {
	path, err = a.ui.PickDirectory(fileDialogOptions{
		Title:                title,
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
	return path, err == nil && path == "", err
}
