package main

import (
	"context"

	pdtdarwin "PDT/internal/printer/darwin"
)

// CupsWebUIResult is CupsWebUIStatus/SetCupsWebUI's own result shape -
// Enabled mirrors SpoolerResult's own State, for the CUPS control button's
// own coloring on macOS (the Spooler button's own role on Windows, which
// CUPS has no serviceable spooler-process equivalent for - see
// spooler_windows.go).
type CupsWebUIResult struct {
	Enabled bool   `json:"enabled"`
	Error   string `json:"error"`
}

// CupsWebUIStatus reports whether CUPS' own web admin UI is currently
// enabled - the CUPS control button's own color on startup, and whenever the
// frontend wants to refresh it without performing an action. Unprivileged;
// see pdtdarwin.WebInterfaceEnabled's own doc comment.
func (a *App) CupsWebUIStatus() CupsWebUIResult {
	enabled, err := pdtdarwin.WebInterfaceEnabled(context.Background())
	if err != nil {
		return CupsWebUIResult{Error: err.Error()}
	}
	return CupsWebUIResult{Enabled: enabled}
}

// SetCupsWebUI turns CUPS' own web admin UI (http://localhost:631) on or
// off - `cupsctl WebInterface=yes`/`=no`, prompting for administrator
// privileges the same way Deploy's own lpadmin calls already do. Opens the
// web UI in the system default browser right after a successful enable
// (Ken, 2026-10-01) - skipped on disable, and on a failed enable, since
// there'd be nothing there to show yet.
func (a *App) SetCupsWebUI(enabled bool) CupsWebUIResult {
	if err := pdtdarwin.SetWebInterfaceEnabled(context.Background(), enabled); err != nil {
		return CupsWebUIResult{Error: err.Error()}
	}
	result := a.CupsWebUIStatus()
	if enabled && result.Error == "" && result.Enabled {
		a.ui.OpenURL("http://localhost:631")
	}
	return result
}
