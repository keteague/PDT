package main

import (
	"embed"
	"os"
	"path/filepath"
)

// sevenZipAssets embeds a fixed, versioned copy of 7-Zip's own 7z.exe/7z.dll
// (plus its license text) - the only thing confirmed to correctly extract
// Lexmark's self-extracting RAR driver package (see the README's "Lexmark"
// section: Go's standard library has no RAR reader at all, the one pure-Go
// library evaluated silently corrupts exactly the files this needs, and
// 7-Zip's own redistributable "Extra" console package doesn't include RAR
// support either - only the full 7z.dll does). Redistribution is permitted
// under 7-Zip's own license (LGPL + an "unRAR restriction" limited to
// barring use of the code to build a RAR *compressor* - not redistribution
// of the decoder), provided the license text travels with the binaries,
// which is why License.txt is embedded and extracted alongside them.
//
// Kept in a cross-platform file, not sevenzip_windows.go: a macOS build
// never runs 7z.exe (nothing here extracts a RAR-based driver package on
// macOS), but Write to Flash Drive still needs these exact bytes bundled in
// so it can lay down a real Windows tools/7zip folder - the same one a
// Windows-built PDT would produce - so a flash drive built on a Mac is just
// as usable on a locked-down Windows machine (see writePortablePDTTo's own
// doc comment on the write-protected-flash-drive fallback this enables) as
// one built on Windows itself.
//
//go:embed third_party/7zip/7z.exe third_party/7zip/7z.dll third_party/7zip/License.txt
var sevenZipAssets embed.FS

// writeSevenZipAssets writes the embedded 7z.exe/7z.dll/License.txt into
// destDir, creating it if needed. Skips rewriting a file already there at
// the same size - Windows' ensureSevenZipExtracted calls this on every
// startup, and rewriting ~2.5MB unconditionally every launch would be
// wasteful. Not a full content hash - the same size-as-proxy-for-identical
// reasoning copyTreeMerge already uses elsewhere in this codebase, good
// enough since these bytes only ever change by shipping a new PDT build
// with a different embedded 7-Zip version.
func writeSevenZipAssets(destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"7z.exe", "7z.dll", "License.txt"} {
		data, err := sevenZipAssets.ReadFile("third_party/7zip/" + name)
		if err != nil {
			return err
		}
		destPath := filepath.Join(destDir, name)
		if info, statErr := os.Stat(destPath); statErr == nil && info.Size() == int64(len(data)) {
			continue
		}
		if err := os.WriteFile(destPath, data, 0o755); err != nil {
			return err
		}
	}
	return nil
}
