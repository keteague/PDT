package main

import (
	"context"

	pdtdarwin "PDT/internal/printer/darwin"
)

// InstallRosettaResult is InstallRosetta's own outcome - Error "" means
// success.
type InstallRosettaResult struct {
	Error string `json:"error"`
}

// InstallRosetta installs Rosetta 2 - the CUPS web UI dropdown's own third
// action (Ken, 2026-10-01). Grouped into that same dropdown only because
// it's the one other toolbar slot macOS has for an occasional, admin-facing
// machine-setup action - unrelated to CUPS itself, and doesn't touch that
// button's own on/off color state at all (see btnCupsWebUI's own frontend
// wiring, main.js).
func (a *App) InstallRosetta() InstallRosettaResult {
	if err := pdtdarwin.InstallRosetta(context.Background()); err != nil {
		return InstallRosettaResult{Error: err.Error()}
	}
	return InstallRosettaResult{}
}
