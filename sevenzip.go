package main

import (
	"embed"
	"os"
	"path/filepath"

	"PDT/internal/driver"
)

// sevenZipAssets embeds a fixed, versioned copy of 7-Zip's own 7z.exe/7z.dll
// (plus its license text) - the only thing on this machine confirmed to
// correctly extract Lexmark's self-extracting RAR driver package. See the
// README's "Lexmark" section for why: Go's standard library has no RAR
// reader at all, the one pure-Go library evaluated (nwaples/rardecode)
// silently corrupts exactly the files this needs, and 7-Zip's own
// easily-redistributable "Extra" console package doesn't include RAR support
// either (confirmed directly - it errors "Cannot open the file as archive")
// - only the full 7z.dll does. Redistribution is permitted under 7-Zip's own
// license (LGPL + an "unRAR restriction" limited to barring use of the code
// to build a RAR *compressor* - not redistribution of the decoder), provided
// the license text travels with the binaries, which is why License.txt is
// embedded and extracted alongside them.
//
//go:embed third_party/7zip/7z.exe third_party/7zip/7z.dll third_party/7zip/License.txt
var sevenZipAssets embed.FS

// ensureSevenZipExtracted writes this build's embedded 7z.exe/7z.dll/License
// out to a stable per-machine cache folder and points driver.SevenZipPath at
// the result, so BuildCatalog can auto-extract a self-extracting RAR package
// it finds. Best-effort: any failure just leaves SevenZipPath unset, which
// skips that auto-extraction entirely rather than failing startup over it -
// the rest of the catalog scan doesn't depend on it.
func ensureSevenZipExtracted() {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return
	}
	destDir := filepath.Join(cacheDir, "PDT", "tools", "7zip")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return
	}

	for _, name := range []string{"7z.exe", "7z.dll", "License.txt"} {
		if err := extractEmbeddedIfStale("third_party/7zip/"+name, filepath.Join(destDir, name)); err != nil {
			return
		}
	}
	driver.SevenZipPath = filepath.Join(destDir, "7z.exe")
}

// extractEmbeddedIfStale writes embeddedPath's content to destPath, skipping
// the write if destPath already exists with the same size - cheap enough to
// call on every startup without rewriting ~2.5MB each time, while still
// picking up a newer bundled 7z.exe/7z.dll after PDT itself is updated (see
// internal/update).
func extractEmbeddedIfStale(embeddedPath, destPath string) error {
	data, err := sevenZipAssets.ReadFile(embeddedPath)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(destPath); statErr == nil && info.Size() == int64(len(data)) {
		return nil
	}
	return os.WriteFile(destPath, data, 0o755)
}
