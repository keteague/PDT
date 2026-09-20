package main

import "os/exec"

// openInTextEditor opens path in Notepad - GitHub issue #15's own explicit
// choice of viewer, and the one plain-text editor guaranteed present on
// every real Windows install (unlike, say, a specific code editor a
// technician may or may not have).
func openInTextEditor(path string) error {
	return exec.Command("notepad.exe", path).Start()
}
