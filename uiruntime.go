package main

// uiRuntime is every call PDT's backend makes into the GUI framework's own
// runtime - events to the frontend, native dialogs, opening a URL, quitting.
// Nothing outside uiruntime_wails.go (plus main.go's own app setup) imports
// Wails directly, so moving to a different Wails major version (v3's
// application.App replaces v2's context-plus-package-functions runtime
// entirely) means writing one new implementation of this interface, not
// touching every file that happens to show a dialog or emit progress.
//
// A dialog method returns path == "" (and a nil error) when the user
// canceled - the same convention Wails v2's own dialog functions use, and
// what every caller already checks for.
type uiRuntime interface {
	Emit(event string, data any)
	OpenURL(url string)
	OpenFile(opts fileDialogOptions) (path string, err error)
	SaveFile(opts fileDialogOptions) (path string, err error)
	PickDirectory(opts fileDialogOptions) (path string, err error)
	// Confirm shows a blocking native Yes/No question and reports whether
	// the user chose Yes.
	Confirm(title, message string) (bool, error)
	Quit()
}

// fileFilter is one file-type entry in a dialog's type dropdown, e.g.
// {"CSV Files (*.csv)", "*.csv"}.
type fileFilter struct {
	DisplayName string
	Pattern     string
}

// fileDialogOptions covers OpenFile/SaveFile/PickDirectory - fields a given
// dialog kind has no use for (DefaultFilename on an open dialog,
// CanCreateDirectories on a file dialog) are simply ignored.
type fileDialogOptions struct {
	Title                string
	DefaultDirectory     string
	DefaultFilename      string
	Filters              []fileFilter
	CanCreateDirectories bool
}

// emit sends an event to the frontend, or does nothing when there's no live
// frontend to send it to (a unit test constructing *App directly, before
// startup has ever run).
func (a *App) emit(event string, data any) {
	if a.ui == nil {
		return
	}
	a.ui.Emit(event, data)
}
