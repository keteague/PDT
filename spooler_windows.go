package main

import "PDT/internal/spooler"

// SpoolerResult is StartSpooler/StopSpooler/RestartSpooler/SpoolerStatus's
// outcome - State is one of "running"/"stopped"/"pending" (see
// spooler.Status), used by the frontend to color the Spooler button.
// Dependents lists the services that depend on the spooler which the action
// also stopped/started (display names - see spooler.Result), and Warnings
// any dependent that couldn't be brought back up; both are just logged.
type SpoolerResult struct {
	State      string   `json:"state"`
	Error      string   `json:"error"`
	Dependents []string `json:"dependents"`
	Warnings   []string `json:"warnings"`
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

// actionResult is statusResult plus what the action itself reported.
func actionResult(res spooler.Result, err error) SpoolerResult {
	if err != nil {
		return SpoolerResult{Error: err.Error()}
	}
	out := statusResult()
	out.Dependents = res.Dependents
	out.Warnings = res.Warnings
	return out
}

// SpoolerStatus reports the Print Spooler service's current state, for the
// Spooler button's own color on startup and whenever the frontend wants to
// refresh it without performing an action.
func (a *App) SpoolerStatus() SpoolerResult {
	return statusResult()
}

// StartSpooler starts the Windows Print Spooler service, plus any dependent
// services an earlier StopSpooler took down.
func (a *App) StartSpooler() SpoolerResult {
	return actionResult(spooler.Start())
}

// StopSpooler stops the Windows Print Spooler service, stopping every
// running service that depends on it first.
func (a *App) StopSpooler() SpoolerResult {
	return actionResult(spooler.Stop())
}

// RestartSpooler stops then starts the Windows Print Spooler service and its
// running dependents - a manual escape hatch for a stuck print object/jammed
// queue, the same fix a technician would reach for via services.msc or
// `net stop/start spooler`.
func (a *App) RestartSpooler() SpoolerResult {
	return actionResult(spooler.Restart())
}
