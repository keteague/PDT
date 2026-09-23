//go:build !windows

package driver

import "os/exec"

// hideConsoleWindow is a no-op off Windows - see hidewindow_windows.go. Kept
// as a cross-platform no-op call (rather than a Windows-only call site) so
// the handful of exec.Command calls that shell out to 7z.exe/msiexec.exe in
// plain cross-platform files (msi.go, sfx.go, kyoceraexe.go) need no
// build-tag branching of their own.
func hideConsoleWindow(cmd *exec.Cmd) {}
