package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSelfExtractingRar(t *testing.T) {
	dir := t.TempDir()

	rar5 := filepath.Join(dir, "rar5.exe")
	// A native SFX stub of arbitrary bytes, then the RAR5 signature well past
	// the start - mirrors a real self-extracting package, where the signature
	// sits a few hundred KB into the file, not at offset zero.
	data := append([]byte("MZ"), make([]byte, 4096)...)
	data = append(data, []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}...)
	if err := os.WriteFile(rar5, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingRar(rar5) {
		t.Error("expected a file containing the RAR5 signature to be detected as a self-extracting RAR")
	}

	rar4 := filepath.Join(dir, "rar4.exe")
	data4 := append([]byte("MZ"), []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00}...)
	if err := os.WriteFile(rar4, data4, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingRar(rar4) {
		t.Error("expected a file containing the older RAR 1.5-4.x signature to be detected too")
	}

	plain := filepath.Join(dir, "plain.exe")
	if err := os.WriteFile(plain, []byte("MZ this is just an ordinary executable, not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isSelfExtractingRar(plain) {
		t.Error("an ordinary .exe with no RAR signature should not be detected as a self-extracting RAR")
	}
}

func TestEnsureRarSfxExtracted_NoOpWithoutSevenZipConfigured(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = ""
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ensureRarSfxExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "Foo")); !os.IsNotExist(err) {
		t.Error("expected no extraction to happen with SevenZipPath unset")
	}
}

func TestEnsureRarSfxExtracted_SkipsAlreadyExtracted(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = "some-path-that-would-fail-if-actually-invoked.exe"
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The destination folder already exists (however it got there - a prior
	// run of this, or a manual extraction) - extraction should be skipped
	// entirely, never even attempting to invoke SevenZipPath.
	if err := os.Mkdir(filepath.Join(dir, "Foo"), 0o755); err != nil {
		t.Fatal(err)
	}

	ensureRarSfxExtracted(dir)
	// No assertion needed beyond "this didn't panic/hang trying to exec a
	// bogus SevenZipPath" - if the skip-when-already-extracted check didn't
	// fire, extractRarSfx would have tried (and failed) to run the bogus
	// path, and os.RemoveAll would have removed the Foo/ folder afterward.
	if _, err := os.Stat(filepath.Join(dir, "Foo")); err != nil {
		t.Error("expected the already-extracted Foo/ folder to be left alone, not removed")
	}
}
