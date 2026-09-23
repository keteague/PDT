//go:build windows

package driver

import (
	"os/exec"
	"syscall"
)

// hideConsoleWindow stops cmd's own console window from flashing up when PDT
// (a GUI app with no console of its own) launches a console tool like
// 7z.exe or msiexec.exe - confirmed live: every archive extraction during a
// Refresh/Rescan (or a deploy needing on-demand extraction) popped up its
// own visible console window (Ken, 2026-09-22). CREATE_NO_WINDOW (what
// HideWindow sets) only ever suppresses the spawned process's own window; it
// has no effect on what the process prints - CombinedOutput still captures
// it exactly as before.
func hideConsoleWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
