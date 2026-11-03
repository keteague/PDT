package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeVersioned writes a stand-in app file whose contents are its version,
// and readFakeVersion reads one back - so these tests exercise
// syncAppToDrive's decisions on any platform, without real PE/plist files.
func fakeVersioned(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(version), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFakeVersion(path string) (string, bool) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "VERSION")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func fileApp(src, version string) flashApp {
	return flashApp{label: "PDT.exe (Windows)", name: winExeName, src: src, version: version, readVersion: readFakeVersion}
}

func TestSyncAppToDrive_Decisions(t *testing.T) {
	cases := []struct {
		name       string
		onDrive    string // "" = no copy on the drive
		laptop     string
		wantOnDisk string
		wantLevel  string
		wantText   string
	}{
		{"missing is added", "", "0.9.68", "0.9.68", "OK", "Added"},
		{"older is updated", "0.9.60", "0.9.68", "0.9.68", "OK", "Updated"},
		{"same is left", "0.9.68", "0.9.68", "0.9.68", "OK", "is current"},
		{"newer is never downgraded", "0.9.70", "0.9.68", "0.9.70", "WARN", "newer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), winExeName)
			fakeVersioned(t, src, c.laptop)
			drive := t.TempDir()
			if c.onDrive != "" {
				fakeVersioned(t, filepath.Join(drive, winExeName), c.onDrive)
			}

			note, err := syncAppToDrive(context.Background(), drive, fileApp(src, c.laptop), nil)

			if err != nil {
				t.Fatal(err)
			}
			if got, _ := readFakeVersion(filepath.Join(drive, winExeName)); got != c.wantOnDisk {
				t.Errorf("drive copy is v%s, want v%s", got, c.wantOnDisk)
			}
			if note.Level != c.wantLevel || !strings.Contains(note.Text, c.wantText) {
				t.Errorf("note = %+v, want %s containing %q", note, c.wantLevel, c.wantText)
			}
			if _, err := os.Stat(filepath.Join(drive, "."+winExeName+".new")); !os.IsNotExist(err) {
				t.Error("staging file left behind")
			}
		})
	}
}

// A drive copy whose version can't be read is replaced, and says so.
func TestSyncAppToDrive_UnreadableDriveCopyIsReplaced(t *testing.T) {
	src := filepath.Join(t.TempDir(), winExeName)
	fakeVersioned(t, src, "0.9.68")
	drive := t.TempDir()
	fakeVersioned(t, filepath.Join(drive, winExeName), "")

	note, err := syncAppToDrive(context.Background(), drive, fileApp(src, "0.9.68"), nil)

	if err != nil || !strings.Contains(note.Text, "couldn't be read") {
		t.Fatalf("note = %+v, err = %v", note, err)
	}
	if got, _ := readFakeVersion(filepath.Join(drive, winExeName)); got != "0.9.68" {
		t.Errorf("drive copy is %q, want 0.9.68", got)
	}
}

// An unreadable version on the laptop never overwrites an existing copy.
func TestSyncAppToDrive_UnknownLaptopVersionLeavesExisting(t *testing.T) {
	src := filepath.Join(t.TempDir(), winExeName)
	fakeVersioned(t, src, "")
	drive := t.TempDir()
	fakeVersioned(t, filepath.Join(drive, winExeName), "0.9.60")

	note, err := syncAppToDrive(context.Background(), drive, fileApp(src, ""), nil)

	if err != nil || note.Level != "WARN" {
		t.Fatalf("note = %+v, err = %v; want a WARN and no error", note, err)
	}
	if got, _ := readFakeVersion(filepath.Join(drive, winExeName)); got != "0.9.60" {
		t.Errorf("drive copy changed to %q", got)
	}
}

// A .app bundle is replaced whole: files only the old version had are gone.
func TestSyncAppToDrive_BundleIsReplacedNotMerged(t *testing.T) {
	src := filepath.Join(t.TempDir(), macAppBundleName)
	fakeVersioned(t, filepath.Join(src, "VERSION"), "0.9.68")
	fakeVersioned(t, filepath.Join(src, "Contents", "MacOS", "PDT"), "new binary")
	drive := t.TempDir()
	old := filepath.Join(drive, macAppBundleName)
	fakeVersioned(t, filepath.Join(old, "VERSION"), "0.9.60")
	fakeVersioned(t, filepath.Join(old, "Contents", "Resources", "stale.dat"), "x")

	app := flashApp{label: "PDT.app (macOS)", name: macAppBundleName, src: src, version: "0.9.68", isDir: true, readVersion: readFakeVersion}
	if _, err := syncAppToDrive(context.Background(), drive, app, nil); err != nil {
		t.Fatal(err)
	}

	if got, _ := readFakeVersion(old); got != "0.9.68" {
		t.Errorf("bundle is v%s, want v0.9.68", got)
	}
	if fileExists(filepath.Join(old, "Contents", "Resources", "stale.dat")) {
		t.Error("a file only the old version had survived - bundle was merged, not replaced")
	}
	for _, leftover := range []string{"." + macAppBundleName + ".new", "." + macAppBundleName + ".old"} {
		if dirExists(filepath.Join(drive, leftover)) {
			t.Errorf("%s left behind", leftover)
		}
	}
}

// A portable copy syncing onto its own drive leaves itself alone.
func TestSyncAppToDrive_RunningCopyOnSameDrive(t *testing.T) {
	drive := t.TempDir()
	self := filepath.Join(drive, winExeName)
	fakeVersioned(t, self, "0.9.68")

	note, err := syncAppToDrive(context.Background(), drive, fileApp(self, "0.9.68"), nil)

	if err != nil || !strings.Contains(note.Text, "running now") {
		t.Fatalf("note = %+v, err = %v", note, err)
	}
}
