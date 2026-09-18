package main

import (
	"os"
	goruntime "runtime"
	"testing"
)

// absPathOutsideHomeForOS returns a real absolute path, in this OS's own
// syntax, that isn't under the current test-runner's own home directory -
// resolveHomeRelative's own "already absolute, leave it alone" case (a
// tech's own explicit Browse pick to a network share, a different drive,
// etc.) - mirrors absPathForOS (resolveexerelative_test.go)'s own reasoning
// for why a literal hardcoded for one OS isn't actually absolute on the
// other.
func absPathOutsideHomeForOS() string {
	if goruntime.GOOS == "windows" {
		return `D:\Site Surveys\Preinstall`
	}
	return "/Volumes/SiteSurveys/Preinstall"
}

func TestResolveHomeRelative_AbsolutePathUnchanged(t *testing.T) {
	abs := absPathOutsideHomeForOS()
	if got := resolveHomeRelative(abs); got != abs {
		t.Errorf("resolveHomeRelative(%q) = %q, want unchanged", abs, got)
	}
}

func TestResolveHomeRelative_EmptyPathUnchanged(t *testing.T) {
	if got := resolveHomeRelative(""); got != "" {
		t.Errorf(`resolveHomeRelative("") = %q, want ""`, got)
	}
}

// TestResolveHomeRelative_RelativePathResolvedAgainstHomeDir is the real
// scenario this exists for (see settings.go's own defaultPreinstallBasePath
// doc comment): the same forward-slash-stored value resolves correctly
// against whichever user's own home directory is actually running PDT right
// now, not a value baked in on whichever machine first computed it.
func TestResolveHomeRelative_RelativePathResolvedAgainstHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := home + string(os.PathSeparator) + "Documents" + string(os.PathSeparator) + "Preinstall"
	if got := resolveHomeRelative("Documents/Preinstall"); got != want {
		t.Errorf(`resolveHomeRelative("Documents/Preinstall") = %q, want %q`, got, want)
	}
}

func TestDefaultPreinstallBasePath_IsHomeRelativeNotAbsolute(t *testing.T) {
	if got := defaultPreinstallBasePath(); got != "Documents/Preinstall" {
		t.Errorf("defaultPreinstallBasePath() = %q, want the bare relative form, never a resolved absolute path", got)
	}
}
