package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// pickFolderDialog is PickFolder's (app.go) platform hook on macOS - shows a
// native folder-choose dialog via AppleScript's "choose folder" (through
// osascript) instead of Wails' own runtime.OpenDirectoryDialog.
//
// Why: every Wails-bound method call - PickFolder included - runs on its own
// freshly spawned goroutine, never the process's real main thread (confirmed
// in Wails' own darwin frontend.go: each JS-to-Go call is dispatched via a
// bare `go func(){...}()`). AppKit's NSOpenPanel is only documented-safe to
// drive from the main thread; calling its beginSheetModalForWindow off-thread
// is what silently swallowed every click on Settings > General's "..."
// buttons - the sheet never actually appeared, and Wails' own darwin
// dialog.go surfaced no error either, since it just blocks forever reading
// the response channel the sheet's (never-fired) completion handler would
// have sent on. Wails v2 is end-of-life - no newer release to pick up a fix
// from - and correctly patching its own Objective-C would mean splitting
// "present the sheet" from "wait for the result" so the main thread's run
// loop stays free to actually deliver that result (blocking the main thread
// for the whole wait would just trade one hang for a guaranteed one).
// osascript sidesteps all of this: "choose folder" runs in its own process,
// with its own main thread and run loop, so it always shows.
//
// "choose folder" run bare like this belongs to no particular app, and
// macOS attributes its window to Finder - activating Finder as a side
// effect, which visibly buries PDT's own window (confirmed live: Settings
// itself appeared to vanish, since the modal is just HTML inside PDT's own
// now-backgrounded window). The frontApp capture/reactivate below - a plain
// AppleScript idiom for this exact "choose folder steals focus" behavior -
// records whatever was frontmost (PDT, mid-click) before the dialog runs and
// explicitly reactivates it afterward, on both the success and Cancel paths.
func (a *App) pickFolderDialog(title, defaultDir string) (path string, canceled bool, err error) {
	chooseFolder := "choose folder with prompt " + appleScriptString(title)
	if defaultDir != "" && dirExists(defaultDir) {
		chooseFolder += " default location (POSIX file " + appleScriptString(defaultDir) + ")"
	}
	lines := []string{
		"set frontApp to path to frontmost application as text",
		"try",
		"set folderPath to POSIX path of (" + chooseFolder + ")",
		"on error errText number errNum",
		"tell application frontApp to activate",
		"error errText number errNum",
		"end try",
		"tell application frontApp to activate",
		"folderPath",
	}
	args := make([]string, 0, len(lines)*2)
	for _, line := range lines {
		args = append(args, "-e", line)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("osascript", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		// AppleScript error -128 is "User canceled." - choose folder's own
		// Cancel button, not a real failure.
		if strings.Contains(stderr.String(), "(-128)") {
			return "", true, nil
		}
		return "", false, fmt.Errorf("choose folder: %s", strings.TrimSpace(stderr.String()))
	}
	// "POSIX path of" always adds a trailing "/" for a directory (AppleScript's
	// own convention, not Windows' runtime.OpenDirectoryDialog's) - cleaned so
	// Settings displays/stores the same shape on either platform.
	return filepath.Clean(strings.TrimSpace(stdout.String())), false, nil
}

// appleScriptString quotes s as an AppleScript string literal.
func appleScriptString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
