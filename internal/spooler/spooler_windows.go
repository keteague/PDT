// Package spooler starts, stops, and restarts the Windows Print Spooler
// service ("Spooler") - a manual escape hatch for when a print object gets
// stuck (a hung deploy, a jammed queue) and the fix is the same one a
// technician would reach for by hand via services.msc or `net stop spooler`.
//
// Services that depend on the spooler (Fax, and the helper services printer
// vendors' own software packages install) are handled too, the same way
// services.msc does it: the Service Control Manager refuses to stop a
// service while anything depending on it is still running, so Stop first
// stops every running dependent - recursively, deepest first - and Start
// brings back the ones Stop took down. The services the spooler itself
// depends on (RPCSS, HTTP) are never touched: the SCM already starts those
// on its own when the spooler starts, and stopping them would take down far
// more than printing.
package spooler

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "Spooler"

// waitTimeout bounds how long Stop/Start wait for each service to actually
// reach the target state - the spooler normally responds in well under a
// second, but a wedged one is exactly the case this package exists for, so
// this can't wait forever.
const waitTimeout = 15 * time.Second

// Result reports the dependent services an action stopped or started
// alongside the spooler itself (display names, in the order acted on), plus
// any dependent that couldn't be brought back up - reported rather than
// failing the whole action, since the spooler itself is up by then.
type Result struct {
	Dependents []string
	Warnings   []string
}

var (
	// mu serializes Start/Stop/Restart - two overlapping clicks must never
	// interleave their stop/start sequences.
	mu sync.Mutex

	// stoppedDependents is every dependent service a Stop in this process
	// took down (service names, in stop order) that hasn't been started
	// again yet - what the next Start restores. Only services that were
	// actually running beforehand ever land here, so Start never turns on
	// something the machine had stopped on purpose.
	stoppedDependents []string
)

// Start starts the spooler service, if it isn't already running, then
// restarts any dependent services a previous Stop took down.
func Start() (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	return start()
}

// Stop stops every running service that depends on the spooler (deepest
// first), then the spooler itself.
func Stop() (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	return stop()
}

// Restart stops then starts the spooler service, along with its running
// dependents - Windows' Service Control Manager has no atomic "restart" of
// its own, so this is the same two-step sequence a technician would perform
// by hand. Result.Dependents lists the dependents restarted.
func Restart() (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	if _, err := stop(); err != nil {
		return Result{}, err
	}
	return start()
}

func start() (Result, error) {
	m, err := mgr.Connect()
	if err != nil {
		return Result{}, fmt.Errorf("connecting to the service manager: %w", err)
	}
	defer m.Disconnect()

	if err := startService(m, serviceName); err != nil {
		return Result{}, err
	}

	// Reverse stop order: every service starts only after whatever it
	// depends on is already running.
	var res Result
	for i := len(stoppedDependents) - 1; i >= 0; i-- {
		name := stoppedDependents[i]
		if err := startService(m, name); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not restart dependent service %s: %v", displayName(m, name), err))
			continue
		}
		res.Dependents = append(res.Dependents, displayName(m, name))
	}
	stoppedDependents = nil
	return res, nil
}

func stop() (Result, error) {
	m, err := mgr.Connect()
	if err != nil {
		return Result{}, fmt.Errorf("connecting to the service manager: %w", err)
	}
	defer m.Disconnect()

	order, err := stopOrder(serviceName, func(name string) ([]string, error) {
		return activeDependents(m, name)
	})
	if err != nil {
		return Result{}, err
	}

	var res Result
	var stoppedNow []string
	for _, name := range order {
		if err := stopService(m, name); err != nil {
			// Don't leave the machine with some dependents down and the
			// spooler still up - put back what this call already stopped.
			for i := len(stoppedNow) - 1; i >= 0; i-- {
				_ = startService(m, stoppedNow[i])
			}
			return Result{}, fmt.Errorf("stopping dependent service %s: %w", displayName(m, name), err)
		}
		stoppedNow = append(stoppedNow, name)
		res.Dependents = append(res.Dependents, displayName(m, name))
	}
	stoppedDependents = appendNew(stoppedDependents, stoppedNow)

	if err := stopService(m, serviceName); err != nil {
		return res, err
	}
	return res, nil
}

// activeDependents lists the running services that depend on name.
func activeDependents(m *mgr.Mgr, name string) ([]string, error) {
	s, err := m.OpenService(name)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.ListDependentServices(svc.Active)
}

func startService(m *mgr.Mgr, name string) error {
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("opening the %q service: %w", name, err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying %q service status: %w", name, err)
	}
	if status.State == svc.Running {
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("starting %q service: %w", name, err)
	}
	return waitForState(s, name, svc.Running)
}

func stopService(m *mgr.Mgr, name string) error {
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("opening the %q service: %w", name, err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying %q service status: %w", name, err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("stopping %q service: %w", name, err)
	}
	return waitForState(s, name, svc.Stopped)
}

func waitForState(s *mgr.Service, name string, want svc.State) error {
	deadline := time.Now().Add(waitTimeout)
	for {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("querying %q service status: %w", name, err)
		}
		if status.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %q service to reach state %v (currently %v)", name, want, status.State)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// displayName returns a service's friendly name for the Log panel (e.g.
// "HP Print Scan Doctor Service" rather than "HPPrintScanDoctorService"),
// falling back to the service name itself.
func displayName(m *mgr.Mgr, name string) string {
	s, err := m.OpenService(name)
	if err != nil {
		return name
	}
	defer s.Close()
	cfg, err := s.Config()
	if err != nil || cfg.DisplayName == "" {
		return name
	}
	return cfg.DisplayName
}

// appendNew appends each of add not already in list, keeping list's order -
// a second Stop (with the spooler already stopped, so nothing new found)
// must not forget or duplicate what an earlier Stop recorded.
func appendNew(list, add []string) []string {
	seen := make(map[string]bool, len(list))
	for _, n := range list {
		seen[n] = true
	}
	for _, n := range add {
		if !seen[n] {
			list = append(list, n)
			seen[n] = true
		}
	}
	return list
}

// Status reports the spooler service's current state as one of "running",
// "stopped", or "pending" (StartPending/StopPending/PausePending/
// ContinuePending/Paused all collapse to this - the frontend only ever needs
// "still settling" to show its own transitional color, not which specific
// pending state that is). Returned as a plain string rather than svc.State
// so nothing outside this package needs to import golang.org/x/sys/windows/svc
// just to read a status.
func Status() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("connecting to the service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return "", fmt.Errorf("opening the %q service: %w", serviceName, err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "", fmt.Errorf("querying %q service status: %w", serviceName, err)
	}
	switch status.State {
	case svc.Running:
		return "running", nil
	case svc.Stopped:
		return "stopped", nil
	default:
		return "pending", nil
	}
}
