package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebviewUserDataPathDefault(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	webviewDataNotice = ""
	want := filepath.Join(os.Getenv("APPDATA"), "PDT.exe")
	if got := webviewUserDataPath(); got != want {
		t.Errorf("webviewUserDataPath = %q, want %q", got, want)
	}
	if webviewDataNotice != "" {
		t.Errorf("no fallback expected, got notice %q", webviewDataNotice)
	}
}

func TestWebviewUserDataPathFallsBack(t *testing.T) {
	// An unwritable %AppData% must not be returned.
	t.Setenv("APPDATA", `\\?\Z:\definitely\not\here`)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	webviewDataNotice = ""
	t.Cleanup(func() { webviewDataNotice = "" })
	want := filepath.Join(os.Getenv("LOCALAPPDATA"), "PDT", "WebView2")
	if got := webviewUserDataPath(); got != want {
		t.Errorf("webviewUserDataPath = %q, want %q", got, want)
	}
	if webviewDataNotice == "" {
		t.Error("expected a fallback notice")
	}
}

// In a normal session (the test runner's own), PDT's account is the
// session's account.
func TestRunningAsOtherSessionUserFalseInOwnSession(t *testing.T) {
	if other, sess := runningAsOtherSessionUser(); other {
		t.Errorf("reported a different session user (%q) in the runner's own session", sess)
	}
}

func TestCurrentUserKeyIsSID(t *testing.T) {
	if k := currentUserKey(); len(k) < 4 || k[:4] != "S-1-" {
		t.Errorf("currentUserKey = %q, want a SID", k)
	}
}
