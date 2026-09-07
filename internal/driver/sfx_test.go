package driver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSelfExtractingArchive(t *testing.T) {
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
	if !isSelfExtractingArchive(rar5) {
		t.Error("expected a file containing the RAR5 signature to be detected as a self-extracting archive")
	}

	rar4 := filepath.Join(dir, "rar4.exe")
	data4 := append([]byte("MZ"), []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x00}...)
	if err := os.WriteFile(rar4, data4, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(rar4) {
		t.Error("expected a file containing the older RAR 1.5-4.x signature to be detected too")
	}

	sevenZip := filepath.Join(dir, "7z.exe")
	data7z := append([]byte("MZ"), make([]byte, 4096)...)
	data7z = append(data7z, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}...)
	if err := os.WriteFile(sevenZip, data7z, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(sevenZip) {
		t.Error("expected a file containing the 7z signature to be detected too - confirmed against a real Konica Minolta package packaged this way")
	}

	zip := filepath.Join(dir, "zip.exe")
	dataZip := append([]byte("MZ"), make([]byte, 4096)...)
	dataZip = append(dataZip, []byte{'P', 'K', 0x03, 0x04}...)
	if err := os.WriteFile(zip, dataZip, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isSelfExtractingArchive(zip) {
		t.Error("expected a file containing the Zip local file header signature to be detected too")
	}

	plain := filepath.Join(dir, "plain.exe")
	if err := os.WriteFile(plain, []byte("MZ this is just an ordinary executable, not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isSelfExtractingArchive(plain) {
		t.Error("an ordinary .exe with no known archive signature should not be detected as self-extracting")
	}
}

func TestEnsureSfxArchivesExtracted_NoOpWithoutSevenZipConfigured(t *testing.T) {
	old := SevenZipPath
	SevenZipPath = ""
	defer func() { SevenZipPath = old }()

	dir := t.TempDir()
	rarPath := filepath.Join(dir, "Foo.exe")
	data := []byte{'R', 'a', 'r', '!', 0x1A, 0x07, 0x01, 0x00}
	if err := os.WriteFile(rarPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	ensureSfxArchivesExtracted(dir)

	if _, err := os.Stat(filepath.Join(dir, "Foo")); !os.IsNotExist(err) {
		t.Error("expected no extraction to happen with SevenZipPath unset")
	}
}

func TestEnsureSfxArchivesExtracted_SkipsAlreadyExtracted(t *testing.T) {
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

	ensureSfxArchivesExtracted(dir)
	// No assertion needed beyond "this didn't panic/hang trying to exec a
	// bogus SevenZipPath" - if the skip-when-already-extracted check didn't
	// fire, extractSfxArchive would have tried (and failed) to run the bogus
	// path, and os.RemoveAll would have removed the Foo/ folder afterward.
	if _, err := os.Stat(filepath.Join(dir, "Foo")); err != nil {
		t.Error("expected the already-extracted Foo/ folder to be left alone, not removed")
	}
}
