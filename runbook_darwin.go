package main

import "os/exec"

// openInTextEditor opens path in TextEdit - GitHub issue #15's own explicit
// choice of viewer, and the one plain-text editor guaranteed present on
// every real macOS install.
func openInTextEditor(path string) error {
	return exec.Command("open", "-a", "TextEdit", path).Start()
}
