package driver

import "fmt"

// ExtractionWarning, when set, is called with a human-readable message every
// time BuildCatalog's own .inf-only archive extraction (ensureZipInfsExtracted/
// ensureMsiInfsExtracted/ensureSfxArchiveInfsExtracted/ensureKyoceraExeInfsExtracted)
// fails to actually produce an .inf from a real local package - previously a
// completely silent failure (os.RemoveAll(destDir) and a plain `return nil`,
// nothing surfaced anywhere). Confirmed live as a real, previously invisible
// gap (2026-09-21): Lexmark's own real driver package extracts down to
// several .msi files via the bundled 7z.exe, but msiexec /a then failed to
// pull a real .inf out of any of them - synced to a Mac (which can't run
// msiexec.exe at all) this looked identical to "macOS just can't do this",
// when the actual failure already happened upstream, on Windows, with zero
// visible sign of it anywhere.
//
// Left nil by default - what `go test` and any build that hasn't wired this
// up gets - every call site below nil-checks first via warnExtraction, the
// same zero-value-means-off convention SevenZipPath itself already uses.
// Wired once, in loadCatalog on both platforms (app_windows.go/
// app_darwin.go), to a.logCatalog("WARN", ...) - PDT's own Log panel, which
// a technician is already watching during a driver scan/refresh.
var ExtractionWarning func(message string)

// warnExtraction is every failure site's own single call-through to
// ExtractionWarning - formats the message and no-ops when nothing's wired up
// to receive it (tests, or any build without a loadCatalog that sets it).
func warnExtraction(format string, args ...any) {
	if ExtractionWarning == nil {
		return
	}
	ExtractionWarning(fmt.Sprintf(format, args...))
}
