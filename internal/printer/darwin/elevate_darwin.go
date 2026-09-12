package darwin

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runPrivileged is the one chokepoint every command needing root (lpadmin,
// installer) goes through - see the README's own elevation note once written:
// unlike Windows' manifest-driven auto-UAC-elevation, a Wails app on macOS
// isn't launched as root, so each privileged operation instead shells out
// through osascript's "do shell script ... with administrator privileges",
// which shows the same native password prompt a user would see running the
// same command from Terminal with sudo. macOS's own Authorization Services
// caches the grant for a few minutes, so a multi-row Deploy run in practice
// prompts once, not once per row.
//
// A canceled/declined prompt surfaces as a plain Go error (osascript's own
// non-zero exit, "execution error: User canceled." on the AppleScript side) -
// callers treat it exactly like any other failed command, no special case.
//
// EVERY \n IN THE CAPTURED STDOUT COMES BACK AS \r: confirmed live via raw
// byte inspection (`od -c`) of a real `do shell script ... with
// administrator privileges` return value - AppleScript normalizes captured
// output to its own classic-Mac \r line-ending convention on the way back,
// universally, regardless of what actually produced the newline (a plain
// `echo`, `printf '...\n'`, anything). Any future caller that needs to parse
// runPrivileged's/runPrivilegedShell's own multi-line stdout must split on
// \r (or both \r and \n, e.g. `strings.FieldsFunc` matching either) - never
// \n alone. Found this chasing real-time install progress out of a
// privileged call (see EnsureDriverInstalled's own doc comment for why that
// whole approach was ultimately abandoned) - a second, more fundamental
// finding from the same investigation: `do shell script`'s own privileged-
// execution mechanism buffers a command's *entire* output until it fully
// exits, no matter how many pipe stages run inside the script - confirmed
// via a fast synthetic test (a 1-second-`sleep`-separated loop) that came
// back with every line stamped identically despite real elapsed time
// between them. There is no way to get genuine incremental output out of
// this mechanism at all - only a fundamentally different one (e.g. an
// elevated script writing progress to a file an unprivileged goroutine tails
// independently) would.
//
// UNVERIFIED FROM A REAL SHIPPED APP, AND LIKELY NEEDS A REAL CODE SIGNATURE:
// confirmed live against a real driver install (a bare `pdtdebugmac` CLI
// binary, built with `go build`/`go run`, running on real macOS 26) that this
// mechanism gets SIGKILLed by AMFI (AppleMobileFileIntegrity) the moment the
// privileged command actually starts running - even after the password
// prompt itself is accepted - when the calling binary is only ad-hoc signed
// (`codesign -s -`, no real certificate chain). The system log is unambiguous
// about why:
//
//	amfid: '<path>' not valid: Error Domain=AppleMobileFileIntegrityError
//	Code=-423 "The file is adhoc signed or signed by an unknown certificate
//	chain"
//
// A hand-typed `osascript -e '...'` run directly from Terminal.app (a
// properly Apple-signed process, no unsigned binary anywhere in the chain)
// was confirmed to work for the exact same underlying `installer -pkg ...
// -target /` command with no issue - so the mechanism itself, and this
// package's own command construction/quoting (see quoteShellCommand/
// appleScriptQuote's own round-trip tests against a real shell and real
// osascript), are both confirmed correct. What's unconfirmed is whether a
// real `wails build`-produced .app bundle (what PDT actually ships, not a
// bare CLI executable in /tmp) hits the same AMFI rejection, or whether
// proper bundle structure alone is enough even without a paid Apple
// Developer ID - that can only be tested once the app.go/main-package darwin
// wiring exists to produce a real .app to test against (see this project's
// own follow-up work). If it turns out a real .app bundle hits this too, the
// practical fix is a real Developer ID code signature (and likely
// notarization) for the shipped build - a real cost/process change from the
// Windows side's current unsigned-installer precedent, worth flagging before
// committing to it.
func runPrivileged(ctx context.Context, argv []string) (stdout string, err error) {
	return runPrivilegedShell(ctx, quoteShellCommand(argv))
}

// runPrivilegedShell is runPrivileged's own sibling for a caller that needs
// several commands run together as one elevated call (e.g.
// canoninstall_darwin.go's own install-Core-then-place-the-staged-files
// script) rather than a single command's argv - a plain argv list has no way
// to express that (quoteShellCommand quotes each argument as an inert
// literal, never interpreted as shell syntax). shellCmd is caller-built and
// caller-quoted (see quoteShellCommand for the same single-quote-and-escape
// convention, or singleQuoteShellArg for one value at a time); this only
// adds the "with administrator privileges" wrapping shared with
// runPrivileged, nothing else.
func runPrivilegedShell(ctx context.Context, shellCmd string) (stdout string, err error) {
	script := fmt.Sprintf(`do shell script %s with administrator privileges`, appleScriptQuote(shellCmd))
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("running %q: %w: %s", shellCmd, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("running %q: %w", shellCmd, err)
	}
	return string(out), nil
}

// singleQuoteShellArg wraps one string in single quotes for embedding
// literally in a POSIX shell command, the same escaping quoteShellCommand
// applies per-argument - shared here since runPrivilegedShell callers build
// their own multi-command shell strings by hand rather than a plain argv
// list, but still need each interpolated value quoted just as safely.
func singleQuoteShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteShellCommand builds a single POSIX shell command line from argv, each
// argument wrapped in single quotes with any single quote it contains closed
// out, escaped, and reopened - never naive string concatenation, since these
// arguments include paths and device URIs that can contain spaces and
// shell-meaningful characters.
func quoteShellCommand(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}

// appleScriptQuote wraps s in double quotes for embedding inside an
// AppleScript string literal, escaping the two characters AppleScript's own
// double-quoted strings treat specially (\ and ").
func appleScriptQuote(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
