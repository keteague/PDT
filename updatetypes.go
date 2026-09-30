package main

// UpdateCheckResult is CheckForUpdate's outcome - shared across platforms
// (update_windows.go/update_darwin.go both return it) and by the Windows-only
// bundled-7-Zip check (sevenzip_windows.go's CheckSevenZipUpdate), since the
// frontend's checkForUpdate/checkSevenZipUpdate render both the same way.
type UpdateCheckResult struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	ReleaseURL     string `json:"releaseUrl"`
	AssetURL       string `json:"assetUrl"`
	Error          string `json:"error"`
}

// ApplyUpdateResult is ApplyUpdate's outcome - shared across platforms
// (update_windows.go/update_darwin.go). Error is "" on success, in which
// case the app has already relaunched itself and this process is about to
// quit - there is nothing further for the frontend to do either way.
type ApplyUpdateResult struct {
	Error string `json:"error"`
}

// AutoUpdateOutcome is one product's automatic check: Checked is false when
// it didn't run this launch (disabled in Settings, not due yet, or running
// from a flash drive), otherwise Result is exactly what the matching manual
// check button would have shown.
type AutoUpdateOutcome struct {
	Checked bool              `json:"checked"`
	Result  UpdateCheckResult `json:"result"`
}

// AutoUpdateResult is AutoUpdateChecks' outcome (autoupdate_windows.go/
// autoupdate_darwin.go). SevenZip is only ever Checked on Windows - macOS
// has no bundled-7-Zip dependency to check.
type AutoUpdateResult struct {
	PDT      AutoUpdateOutcome `json:"pdt"`
	SevenZip AutoUpdateOutcome `json:"sevenZip"`
}
