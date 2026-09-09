package main

import "testing"

// TestAppVersion_TrimmedNonEmpty guards the go:embed VERSION indirection in
// version.go: an accidental blank line, trailing CRLF, or missing VERSION
// file would silently make every version-displaying surface (titlebar,
// About tab, update-check comparison) show/compare against an empty string
// instead of failing loudly at compile time.
func TestAppVersion_TrimmedNonEmpty(t *testing.T) {
	if AppVersion == "" {
		t.Fatal("AppVersion is empty - check the repo-root VERSION file")
	}
	if AppVersion != rawVersion && AppVersion+"\n" != rawVersion && AppVersion+"\r\n" != rawVersion {
		t.Errorf("AppVersion %q does not look like a trimmed form of the embedded VERSION file %q", AppVersion, rawVersion)
	}
}
