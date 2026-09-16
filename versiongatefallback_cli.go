package main

import (
	"fmt"
	"os"

	"PDT/internal/driver"
)

// runMacVersionGateFallbackCLI is issue #12's own performance fix
// (2026-09-16): a real live deploy showed this fallback's own extraction
// work (driver.DriverFootprintForVersionGateFallback) adding ~29 seconds to
// PrepareBatch's planning phase across an 8-row batch - it was being built
// *eagerly*, for every row whose package merely sat in a macOS 14+ folder,
// regardless of whether the real `installer` run would ever actually fail
// and need it (confirmed live: only Ricoh's own package in that batch
// genuinely needed it; every other row paid the same extraction cost for
// nothing). Batching plans its whole combined shell script up front, before
// the one elevated call - there's no way to run more Go code partway
// through that already-running script, so the only way to make this
// genuinely lazy (extraction only happens after `installer` has actually
// failed) is for the script itself to re-invoke this same running PDT
// binary as a subprocess, from inside the elevated shell, only on that
// specific failure - see wrapInstallerWithVersionGateFallback's own updated
// doc comment for the full wiring.
//
// Deliberately does not call the returned cleanup func - the caller (the
// elevated shell script) still needs the printed stage directory to exist
// long enough to `cp` from it; that shell script is responsible for its own
// `rm -rf` afterward. A crash between this printing a path and the shell's
// own cleanup running leaks one temp directory - accepted, matching this
// codebase's own established tolerance for a rare, low-stakes leak (see
// issue #1's own stale-mount leak) over the complexity of coordinating
// cleanup across a process boundary.
//
// Prints nothing but the stage directory's own path to stdout on success
// (exit 0) - the calling shell script captures it directly via command
// substitution. Exit 1, no output, when there's nothing to fall back to
// (the same "honest, unwrappable original error beats a fallback that could
// never succeed" reasoning wrapInstallerWithVersionGateFallback's own doc
// comment already establishes).
func runMacVersionGateFallbackCLI(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: PDT "+driver.MacVersionGateFallbackCLIArg+" <pkgPath>")
		return 1
	}
	stageDir, _, ok := driver.DriverFootprintForVersionGateFallback(args[0])
	if !ok {
		return 1
	}
	fmt.Println(stageDir)
	return 0
}
