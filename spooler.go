package main

import "PDT/internal/spooler"

// SpoolerResult is StartSpooler/StopSpooler/RestartSpooler/SpoolerStatus's
// outcome - State is one of "running"/"stopped"/"pending" (see
// spooler.Status), used by the frontend to color the Spooler button.
type SpoolerResult struct {
	State string `json:"state"`
	Error string `json:"error"`
}

// statusResult re-queries the spooler's actual current state after an
// action, rather than assuming the requested action's own success implies a
// specific end state - Start/Stop/Restart already wait for their own target
// state internally, but querying fresh here is one function instead of
// three copies of the same "now report what actually happened" logic.
func statusResult() SpoolerResult {
	state, err := spooler.Status()
	if err != nil {
		return SpoolerResult{Error: err.Error()}
	}
	return SpoolerResult{State: state}
}

// SpoolerStatus reports the Print Spooler service's current state, for the
// Spooler button's own color on startup and whenever the frontend wants to
// refresh it without performing an action.
func (a *App) SpoolerStatus() SpoolerResult {
	return statusResult()
}

// StartSpooler starts the Windows Print Spooler service.
func (a *App) StartSpooler() SpoolerResult {
	if err := spooler.Start(); err != nil {
		return SpoolerResult{Error: err.Error()}
	}
	return statusResult()
}

// StopSpooler stops the Windows Print Spooler service.
func (a *App) StopSpooler() SpoolerResult {
	if err := spooler.Stop(); err != nil {
		return SpoolerResult{Error: err.Error()}
	}
	return statusResult()
}

// RestartSpooler stops then starts the Windows Print Spooler service - a
// manual escape hatch for a stuck print object/jammed queue, the same fix a
// technician would reach for via services.msc or `net stop/start spooler`.
func (a *App) RestartSpooler() SpoolerResult {
	if err := spooler.Restart(); err != nil {
		return SpoolerResult{Error: err.Error()}
	}
	return statusResult()
}
