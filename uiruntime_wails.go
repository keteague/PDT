package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsRuntime is uiRuntime on Wails v2 - a thin pass-through to
// github.com/wailsapp/wails/v2/pkg/runtime, which keys every call off the
// context Wails hands OnStartup.
type wailsRuntime struct {
	ctx context.Context
}

func newWailsRuntime(ctx context.Context) *wailsRuntime {
	return &wailsRuntime{ctx: ctx}
}

func (w *wailsRuntime) Emit(event string, data any) {
	runtime.EventsEmit(w.ctx, event, data)
}

func (w *wailsRuntime) OpenURL(url string) {
	runtime.BrowserOpenURL(w.ctx, url)
}

func (w *wailsRuntime) OpenFile(opts fileDialogOptions) (string, error) {
	return runtime.OpenFileDialog(w.ctx, toWailsOpenOptions(opts))
}

func (w *wailsRuntime) SaveFile(opts fileDialogOptions) (string, error) {
	return runtime.SaveFileDialog(w.ctx, runtime.SaveDialogOptions{
		Title:            opts.Title,
		DefaultDirectory: opts.DefaultDirectory,
		DefaultFilename:  opts.DefaultFilename,
		Filters:          toWailsFilters(opts.Filters),
	})
}

func (w *wailsRuntime) PickDirectory(opts fileDialogOptions) (string, error) {
	return runtime.OpenDirectoryDialog(w.ctx, toWailsOpenOptions(opts))
}

func (w *wailsRuntime) Confirm(title, message string) (bool, error) {
	result, err := runtime.MessageDialog(w.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "Yes",
		CancelButton:  "No",
	})
	if err != nil {
		return false, err
	}
	return result == "Yes", nil
}

func (w *wailsRuntime) Quit() {
	runtime.Quit(w.ctx)
}

func toWailsOpenOptions(opts fileDialogOptions) runtime.OpenDialogOptions {
	return runtime.OpenDialogOptions{
		Title:                opts.Title,
		DefaultDirectory:     opts.DefaultDirectory,
		DefaultFilename:      opts.DefaultFilename,
		Filters:              toWailsFilters(opts.Filters),
		CanCreateDirectories: opts.CanCreateDirectories,
	}
}

func toWailsFilters(filters []fileFilter) []runtime.FileFilter {
	if filters == nil {
		return nil
	}
	out := make([]runtime.FileFilter, len(filters))
	for i, f := range filters {
		out[i] = runtime.FileFilter{DisplayName: f.DisplayName, Pattern: f.Pattern}
	}
	return out
}
