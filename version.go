package main

// AppVersion is this build's version - shown in the titlebar and Settings'
// About tab. Keep in sync with wails.json's info.productVersion (used for
// the compiled exe's own Win32 version resource; Go code has no way to read
// that back at runtime, hence this separate constant rather than a single
// source of truth).
const AppVersion = "0.2.0"

const (
	appDisplayName = "Printer Deployment Tool"
	appAuthor      = "Ken Teague"
	appRepoURL     = "https://github.com/keteague/PDT"
)
