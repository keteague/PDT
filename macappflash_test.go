package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleShortVersionString</key>
<string>%s</string>
</dict></plist>`

func plistFor(version string) string { return strings.Replace(testInfoPlist, "%s", version, 1) }

// writeMacAppZip builds a zip shaped like the release's PDT-macOS.zip: every
// real entry under a top-level "PDT.app/", plus the __MACOSX sidecar and an
// AppleDouble "._" file a Mac-built zip carries and that must be skipped.
func writeMacAppZip(t *testing.T, path, version string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	files := map[string]string{
		"PDT.app/Contents/Info.plist":     plistFor(version),
		"PDT.app/Contents/MacOS/PDT":      "binary v" + version,
		"PDT.app/Contents/Resources/x":    "res",
		"PDT.app/Contents/._Info.plist":   "appledouble",
		"__MACOSX/PDT.app/Contents/._foo": "sidecar",
	}
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// fakeSource serves a locally-built zip instead of downloading, and counts
// how many times a download was requested.
func fakeSource(t *testing.T, version string) (*macAppSource, *int) {
	t.Helper()
	src := t.TempDir()
	zipPath := filepath.Join(src, "PDT-macOS.zip")
	writeMacAppZip(t, zipPath, version)
	downloads := 0
	return &macAppSource{
		version:  version,
		assetURL: "https://example.invalid/PDT-macOS.zip",
		download: func(url, dest string) (string, error) {
			downloads++
			data, err := os.ReadFile(zipPath)
			if err != nil {
				return "", err
			}
			return dest, os.WriteFile(dest, data, 0o644)
		},
	}, &downloads
}

func TestMacAppInstalledVersion(t *testing.T) {
	app := filepath.Join(t.TempDir(), "PDT.app")
	if _, ok := macAppInstalledVersion(app); ok {
		t.Error("a missing bundle has no version")
	}
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plistFor("0.9.36")), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, ok := macAppInstalledVersion(app); !ok || v != "0.9.36" {
		t.Errorf("macAppInstalledVersion = (%q, %v), want 0.9.36", v, ok)
	}
}

func TestEnsureMacAppOnDrive_AddsMissingApp(t *testing.T) {
	drive := t.TempDir()
	src, downloads := fakeSource(t, "0.9.40")
	defer src.cleanup()

	note, err := ensureMacAppOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *downloads != 1 || !strings.Contains(note.Text, "Added") {
		t.Errorf("expected one download and an 'Added' note, got %d / %q", *downloads, note.Text)
	}
	if v, ok := macAppInstalledVersion(filepath.Join(drive, "PDT.app")); !ok || v != "0.9.40" {
		t.Errorf("installed version = (%q, %v), want 0.9.40", v, ok)
	}
	if _, err := os.Stat(filepath.Join(drive, "PDT.app", "Contents", "MacOS", "PDT")); err != nil {
		t.Errorf("expected the app binary to be extracted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(drive, "PDT.app", "Contents", "._Info.plist")); !os.IsNotExist(err) {
		t.Errorf("AppleDouble sidecar files must be skipped, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(drive, "__MACOSX")); !os.IsNotExist(err) {
		t.Errorf("__MACOSX must not be extracted, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(drive, ".PDT.app.new")); !os.IsNotExist(err) {
		t.Errorf("the staging folder must be gone, got err=%v", err)
	}
}

func TestEnsureMacAppOnDrive_ReplacesOutdatedApp(t *testing.T) {
	drive := t.TempDir()
	old := filepath.Join(drive, "PDT.app", "Contents")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(old, "Info.plist"), []byte(plistFor("0.9.30")), 0o644)
	os.WriteFile(filepath.Join(old, "stale-file"), []byte("x"), 0o644)
	src, _ := fakeSource(t, "0.9.40")
	defer src.cleanup()

	note, err := ensureMacAppOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note.Text, "from v0.9.30 to v0.9.40") {
		t.Errorf("note = %q", note.Text)
	}
	if _, err := os.Stat(filepath.Join(old, "stale-file")); !os.IsNotExist(err) {
		t.Errorf("the old bundle's files must not linger, got err=%v", err)
	}
}

func TestEnsureMacAppOnDrive_LeavesCurrentAppAloneAndDoesNotDownload(t *testing.T) {
	drive := t.TempDir()
	cur := filepath.Join(drive, "PDT.app", "Contents")
	os.MkdirAll(cur, 0o755)
	os.WriteFile(filepath.Join(cur, "Info.plist"), []byte(plistFor("0.9.40")), 0o644)
	os.WriteFile(filepath.Join(cur, "marker"), []byte("keep me"), 0o644)
	src, downloads := fakeSource(t, "0.9.40")
	defer src.cleanup()

	note, err := ensureMacAppOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *downloads != 0 || !strings.Contains(note.Text, "current") {
		t.Errorf("expected no download and a 'current' note, got %d / %q", *downloads, note.Text)
	}
	if _, err := os.Stat(filepath.Join(cur, "marker")); err != nil {
		t.Errorf("a current app must be left untouched: %v", err)
	}
}

// A failed extraction must never cost the drive the app it already had.
func TestExtractMacAppZip_CorruptZipKeepsExistingApp(t *testing.T) {
	drive := t.TempDir()
	cur := filepath.Join(drive, "PDT.app", "Contents")
	os.MkdirAll(cur, 0o755)
	os.WriteFile(filepath.Join(cur, "marker"), []byte("keep me"), 0o644)
	bad := filepath.Join(t.TempDir(), "bad.zip")
	os.WriteFile(bad, []byte("not a zip"), 0o644)

	if err := extractMacAppZip(context.Background(), bad, drive, nil); err == nil {
		t.Fatal("expected an error for a corrupt zip")
	}
	if _, err := os.Stat(filepath.Join(cur, "marker")); err != nil {
		t.Errorf("the existing app must survive a failed update: %v", err)
	}
}

func TestExtractMacAppZip_DownloadIsSharedAcrossDrives(t *testing.T) {
	src, downloads := fakeSource(t, "0.9.40")
	defer src.cleanup()
	for i := 0; i < 3; i++ {
		if _, err := ensureMacAppOnDrive(context.Background(), t.TempDir(), src, nil); err != nil {
			t.Fatal(err)
		}
	}
	if *downloads != 1 {
		t.Errorf("the release zip should be downloaded once for all drives, got %d", *downloads)
	}
}
