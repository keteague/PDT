package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKyoceraExeNameRe(t *testing.T) {
	cases := []struct {
		name        string
		wantMatch   bool
		wantVersion string
	}{
		{"KXDRIVER 8.6A.1412.exe", true, "8.6A.1412"},
		{"KXDriver_8.6.1022.exe", true, "8.6.1022"},
		{"kxdriver 8.7a.0422.exe", true, "8.7a.0422"}, // case-insensitive
		{"Setup.exe", false, ""},
		{"KmInstall.exe", false, ""},
		{"KXDriver.exe", false, ""}, // no separator/version at all
	}
	for _, c := range cases {
		m := kyoceraExeNameRe.FindStringSubmatch(c.name)
		if c.wantMatch && m == nil {
			t.Errorf("kyoceraExeNameRe.FindStringSubmatch(%q) = nil, want a match", c.name)
			continue
		}
		if !c.wantMatch {
			if m != nil {
				t.Errorf("kyoceraExeNameRe.FindStringSubmatch(%q) = %v, want no match", c.name, m)
			}
			continue
		}
		if m[1] != c.wantVersion {
			t.Errorf("kyoceraExeNameRe.FindStringSubmatch(%q) captured %q, want %q", c.name, m[1], c.wantVersion)
		}
	}
}

func TestKyoceraVersionAlreadyExtracted(t *testing.T) {
	dirs := []string{"KXDriver_8.6A.1412", "Archive", "KXDriver_8.6.1022_2"}
	if !kyoceraVersionAlreadyExtracted(dirs, "8.6A.1412") {
		t.Error("expected an exact version match to be found")
	}
	if !kyoceraVersionAlreadyExtracted(dirs, "8.6a.1412") {
		t.Error("expected the match to be case-insensitive")
	}
	if !kyoceraVersionAlreadyExtracted(dirs, "8.6.1022") {
		t.Error("expected a version match against a folder with an extra suffix (substring match)")
	}
	if kyoceraVersionAlreadyExtracted(dirs, "9.0.0000") {
		t.Error("expected no match for a version with no corresponding folder")
	}
}

func TestEnsureKyoceraExesExtracted_NoOpWithoutSevenZipConfigured(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = ""
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "KXDRIVER 8.6A.1412.exe"), []byte("not a real exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	ensureKyoceraExesExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "KXDriver_8.6A.1412")); !os.IsNotExist(err) {
		t.Error("expected no extraction to happen with SevenZipPath unset")
	}
}

func TestEnsureKyoceraExesExtracted_SkipsAlreadyExtractedVersion(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = "some-path-that-would-fail-if-actually-invoked.exe"
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "KXDRIVER 8.6A.1412.exe"), []byte("not a real exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A destination folder already exists for this version, under whatever
	// naming the original manual extraction happened to use - extraction
	// should be skipped entirely, never even attempting to invoke
	// SevenZipPath.
	if err := os.Mkdir(filepath.Join(dir, "KXDriver_8.6A.1412"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureKyoceraExesExtracted(dir)
	// No assertion needed beyond "this didn't try to run a bogus
	// SevenZipPath" - if the skip check didn't fire, extractKyoceraExe would
	// have tried (and failed) to run the bogus path, and the destination
	// would have been removed afterward by the cleanup-on-failure path.
	if _, err := os.Stat(filepath.Join(dir, "KXDriver_8.6A.1412")); err != nil {
		t.Error("expected the already-extracted folder to be left alone, not removed")
	}
}
