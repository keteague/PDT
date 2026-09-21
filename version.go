package main

import (
	_ "embed"
	"strings"
)

// AppVersion is this build's version - shown in the titlebar and Settings'
// About tab. Sourced from the repo-root VERSION file (embedded at compile
// time) rather than a hardcoded literal here, so a version bump only ever
// needs to change that one file instead of drifting independently across
// version.go/wails.json/installer/pdt.iss - confirmed live as a real gap:
// pdt.iss's own #define was left behind at an old version after a bump
// elsewhere went unnoticed. wails.json's info.productVersion (used for the
// compiled exe's own Win32 version resource) can't itself read VERSION -
// Wails has no mechanism for that - so it remains the one field a version
// bump still has to touch by hand; installer/pdt.iss reads VERSION directly
// via its own preprocessor.
//
//go:embed VERSION
var rawVersion string

var AppVersion = strings.TrimSpace(rawVersion)

const (
	appDisplayName = "Printer Deployment Tool"
	appAuthor      = "Ken Teague"
	appRepoURL     = "https://github.com/keteague/PDT"
)

// repoSlug is appRepoURL in GitHub API "owner/name" form - shared by the
// Windows self-update check (update_windows.go) and the flash-drive PDT.app
// check (macappflash.go), which runs on either platform.
func repoSlug() string {
	return strings.TrimPrefix(appRepoURL, "https://github.com/")
}
