package darwin

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuoteShellCommand_RoundTripsThroughRealShell(t *testing.T) {
	cases := [][]string{
		{"echo", "hello", "world"},
		{"echo", "it's a test"},
		{"echo", `"quoted"`},
		{"echo", "path with 'single' and \"double\" quotes"},
		{"echo", "/Volumes/Some Driver's Folder/x.pkg"},
	}
	for _, argv := range cases {
		shellCmd := quoteShellCommand(argv)
		out, err := exec.Command("/bin/sh", "-c", shellCmd).Output()
		if err != nil {
			t.Fatalf("quoteShellCommand(%v) = %q: running it failed: %v", argv, shellCmd, err)
		}
		want := strings.Join(argv[1:], " ")
		if strings.TrimRight(string(out), "\n") != want {
			t.Errorf("quoteShellCommand(%v) = %q: shell echoed %q, want %q", argv, shellCmd, strings.TrimRight(string(out), "\n"), want)
		}
	}
}

// TestAppleScriptQuote_RoundTripsRealElevatePipeline exercises the same two-
// layer composition runPrivileged actually uses (quoteShellCommand's output
// fed into appleScriptQuote), via osascript directly rather than
// administrator-privileges (so it runs unprivileged, no password prompt) -
// proving a value containing single quotes, double quotes, and backslashes
// survives both layers intact.
func TestAppleScriptQuote_RoundTripsRealElevatePipeline(t *testing.T) {
	cases := [][]string{
		{"echo", "hello"},
		{"echo", "it's a test"},
		{"echo", `say "hi"`},
		{"echo", `back\slash`},
	}
	for _, argv := range cases {
		shellCmd := quoteShellCommand(argv)
		script := "do shell script " + appleScriptQuote(shellCmd)
		out, err := exec.Command("osascript", "-e", script).Output()
		if err != nil {
			t.Fatalf("argv=%v shellCmd=%q script=%q: osascript failed: %v", argv, shellCmd, script, err)
		}
		want := argv[1]
		if got := strings.TrimRight(string(out), "\n"); got != want {
			t.Errorf("argv=%v: osascript echoed %q, want %q", argv, got, want)
		}
	}
}
