package darwin

import (
	"context"
	"fmt"
	"runtime"
)

// InstallRosetta installs Rosetta 2 (Apple Silicon's x86_64 translation
// layer) non-interactively - softwareupdate --install-rosetta
// --agree-to-license, the same command Apple's own documentation gives for
// a scripted/unattended install (the plain GUI path otherwise shows its own
// license-acceptance prompt this flag skips). Needs administrator
// privileges the same way every other system-level change in this codebase
// already does - see runPrivileged's own doc comment. Idempotent: re-running
// it against a machine that already has Rosetta just reports that and exits
// cleanly, so no separate "is it already installed" check is needed first.
//
// runtime.GOARCH here is the host machine's own real architecture - unlike
// internal/driver's own PreferredArchTokens (a REMOTE Windows target's
// architecture, which the host running PDT has nothing to do with), Rosetta
// always runs ON this exact machine, translating binaries FOR this exact
// machine, so checking this process's own GOARCH is the correct, intended
// use here.
func InstallRosetta(ctx context.Context) error {
	if runtime.GOARCH != "arm64" {
		return fmt.Errorf("Rosetta is only needed on Apple Silicon Macs")
	}
	_, err := runPrivileged(ctx, []string{"softwareupdate", "--install-rosetta", "--agree-to-license"})
	return err
}
