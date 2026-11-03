package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ownFlashApp is this Mac's running PDT.app bundle, as Sync keeps it current
// on a flash drive (syncAppToDrive). The whole bundle, not the bare
// Contents/MacOS/PDT binary os.Executable() names - Sync used to write just
// that binary to the drive root, as a file called "PDT", which isn't a
// runnable Mac app on its own.
func ownFlashApp() (flashApp, error) {
	exe, err := os.Executable()
	if err != nil {
		return flashApp{}, err
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // .../PDT.app/Contents/MacOS/PDT -> .../PDT.app
	if !strings.EqualFold(filepath.Ext(bundle), ".app") {
		return flashApp{}, fmt.Errorf("not running from a .app bundle (%s)", exe)
	}
	return flashApp{
		label:       "PDT.app (macOS)",
		name:        macAppBundleName,
		src:         bundle,
		version:     AppVersion,
		isDir:       true,
		readVersion: macAppInstalledVersion,
	}, nil
}
