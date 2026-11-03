package main

import "os"

// ownFlashApp is this laptop's running PDT.exe, as Sync keeps it current on
// a flash drive (syncAppToDrive).
func ownFlashApp() (flashApp, error) {
	exe, err := os.Executable()
	if err != nil {
		return flashApp{}, err
	}
	return flashApp{
		label:       "PDT.exe (Windows)",
		name:        winExeName,
		src:         exe,
		version:     AppVersion,
		readVersion: peInstalledVersion,
	}, nil
}
