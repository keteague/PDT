package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resourceBlob builds bytes shaped like a VERSIONINFO string entry: key as
// NUL-terminated UTF-16LE, padding, then the value the same way.
func resourceBlob(key, value string) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x34, 0x00, 0x1c, 0x00, 0x01, 0x00}) // some header bytes
	b.Write(utf16LE(key + "\x00"))
	b.Write([]byte{0x00, 0x00}) // alignment padding
	b.Write(utf16LE(value + "\x00"))
	b.Write([]byte{0x00, 0x00, 0x99, 0x00})
	return b.Bytes()
}

func TestVersionFromResource(t *testing.T) {
	if v, ok := versionFromResource(resourceBlob("ProductVersion", "0.9.37")); !ok || v != "0.9.37" {
		t.Errorf("ProductVersion: got %q, %v", v, ok)
	}
	if v, ok := versionFromResource(resourceBlob("FileVersion", "1.2.3.0")); !ok || v != "1.2.3.0" {
		t.Errorf("FileVersion fallback: got %q, %v", v, ok)
	}
	if _, ok := versionFromResource([]byte("no version info here")); ok {
		t.Error("expected no version from unrelated bytes")
	}
	// A non-numeric value (e.g. a label) isn't a usable version.
	if _, ok := versionFromResource(resourceBlob("ProductVersion", "unknown")); ok {
		t.Error("a non-numeric ProductVersion must not be accepted")
	}
}

// The real Wails-built PDT.exe (when this checkout has built one) carries a
// version this reader can find, and it matches the VERSION file.
func TestPeInstalledVersion_RealBuiltExe(t *testing.T) {
	exe := filepath.Join("build", "bin", "PDT.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip("no build/bin/PDT.exe in this checkout")
	}
	got, ok := peInstalledVersion(exe)
	if !ok {
		t.Fatal("could not read a version out of build/bin/PDT.exe")
	}
	want, err := os.ReadFile("VERSION")
	if err != nil {
		t.Skip("no VERSION file")
	}
	if got != strings.TrimSpace(string(want)) {
		t.Errorf("PDT.exe reports version %q, VERSION file says %q", got, strings.TrimSpace(string(want)))
	}
}

func fakeWinExeSource(t *testing.T, version, content string) (*winExeSource, *int) {
	t.Helper()
	downloads := 0
	dir := t.TempDir()
	return &winExeSource{
		version:  version,
		assetURL: "https://example.invalid/PDT.exe",
		download: func(url, dest string) (string, error) {
			downloads++
			p := filepath.Join(dir, "PDT.exe")
			return p, os.WriteFile(p, []byte(content), 0o644)
		},
	}, &downloads
}

func TestEnsureWinExeOnDrive_AddsWhenMissing(t *testing.T) {
	drive := t.TempDir()
	src, downloads := fakeWinExeSource(t, "9.9.9", "new exe bytes")
	defer src.cleanup()

	note, err := ensureWinExeOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note.Text, "Added PDT.exe v9.9.9") {
		t.Errorf("note = %q", note.Text)
	}
	got, _ := os.ReadFile(filepath.Join(drive, "PDT.exe"))
	if string(got) != "new exe bytes" || *downloads != 1 {
		t.Errorf("drive PDT.exe = %q after %d downloads", got, *downloads)
	}
	if _, err := os.Stat(filepath.Join(drive, ".PDT.exe.new")); err == nil {
		t.Error("staging file left behind")
	}
}

// An existing PDT.exe whose version can't be read (or is older) is replaced.
func TestEnsureWinExeOnDrive_ReplacesUnreadableOrOutdated(t *testing.T) {
	drive := t.TempDir()
	if err := os.WriteFile(filepath.Join(drive, "PDT.exe"), []byte("not a real exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, _ := fakeWinExeSource(t, "9.9.9", "fresh")
	defer src.cleanup()

	note, err := ensureWinExeOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if note.Level != "OK" {
		t.Errorf("note = %+v", note)
	}
	if got, _ := os.ReadFile(filepath.Join(drive, "PDT.exe")); string(got) != "fresh" {
		t.Errorf("PDT.exe not replaced: %q", got)
	}
}

// An up-to-date PDT.exe is left alone and nothing is downloaded.
func TestEnsureWinExeOnDrive_LeavesCurrentExeAlone(t *testing.T) {
	built := filepath.Join("build", "bin", "PDT.exe")
	installed, ok := peInstalledVersion(built)
	if !ok {
		t.Skip("no readable build/bin/PDT.exe in this checkout")
	}
	data, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}
	drive := t.TempDir()
	if err := os.WriteFile(filepath.Join(drive, "PDT.exe"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	src, downloads := fakeWinExeSource(t, installed, "should never be used")
	defer src.cleanup()

	note, err := ensureWinExeOnDrive(context.Background(), drive, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note.Text, "is current") || *downloads != 0 {
		t.Errorf("note = %q, downloads = %d - want current and no download", note.Text, *downloads)
	}
}

func TestEnsureWinExeOnDrive_DownloadFailureIsReportedAndNothingWritten(t *testing.T) {
	drive := t.TempDir()
	src := &winExeSource{version: "9.9.9", download: func(url, dest string) (string, error) {
		return "", os.ErrDeadlineExceeded
	}}
	defer src.cleanup()
	if _, err := ensureWinExeOnDrive(context.Background(), drive, src, nil); err == nil {
		t.Fatal("expected an error")
	}
	if entries, _ := os.ReadDir(drive); len(entries) != 0 {
		t.Errorf("drive was modified: %v", entries)
	}
}
