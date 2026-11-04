package drvsign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func utf16LEWithBOM(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func TestCatalogFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kyomita.cat"), []byte("cat"))
	writeFile(t, filepath.Join(dir, "x64.cat"), []byte("cat"))
	inf := "; comment\r\n[Version]\r\nSignature=\"$Windows NT$\"\r\nCatalogFile= KYOMITA.CAT\r\n" +
		"CatalogFile.NTamd64 = \"x64.cat\" ; 64-bit\r\nCatalogFile.NTarm64=missing.cat\r\n" +
		"[Manufacturer]\r\nCatalogFile=not-in-version.cat\r\n"

	for name, data := range map[string][]byte{
		"ansi.inf":  []byte(inf),
		"utf16.inf": utf16LEWithBOM(inf),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			writeFile(t, path, data)
			got, err := CatalogFiles(path)
			if err != nil {
				t.Fatal(err)
			}
			// Case-insensitive: on Windows the .inf's own spelling
			// (KYOMITA.CAT) resolves directly to the same file.
			if len(got) != 2 || !strings.EqualFold(filepath.Base(got[0]), "kyomita.cat") || filepath.Base(got[1]) != "x64.cat" {
				t.Errorf("CatalogFiles = %v, want kyomita.cat and x64.cat (missing and out-of-section ones skipped)", got)
			}
		})
	}
}
