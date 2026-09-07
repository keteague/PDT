// Package spooler starts, stops, and restarts the Windows Print Spooler
// service ("Spooler") - a manual escape hatch for when a print object gets
// stuck (a hung deploy, a jammed queue) and the fix is the same one a
// technician would reach for by hand via services.msc or `net stop spooler`.
package spooler

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "Spooler"

// waitTimeout bounds how long Stop/Start wait for the service to actually
// reach the target state - the spooler normally responds in well under a
// second, but a wedged one is exactly the case this package exists for, so
// this can't wait forever.
const waitTimeout = 15 * time.Second

func open() (*mgr.Mgr, *mgr.Service, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the service manager: %w", err)
	}
	s, err := m.OpenService(serviceName)
	if err != nil {
		m.Disconnect()
		return nil, nil, fmt.Errorf("opening the %q service: %w", serviceName, err)
	}
	return m, s, nil
}

func waitForState(s *mgr.Service, want svc.State) error {
	deadline := time.Now().Add(waitTimeout)
	for {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("querying %q service status: %w", serviceName, err)
		}
		if status.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %q service to reach state %v (currently %v)", serviceName, want, status.State)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Start starts the spooler service, if it isn't already running.
func Start() error {
	m, s, err := open()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying %q service status: %w", serviceName, err)
	}
	if status.State == svc.Running {
		return nil
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("starting %q service: %w", serviceName, err)
	}
	return waitForState(s, svc.Running)
}

// Stop stops the spooler service, if it isn't already stopped.
func Stop() error {
	m, s, err := open()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("querying %q service status: %w", serviceName, err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("stopping %q service: %w", serviceName, err)
	}
	return waitForState(s, svc.Stopped)
}

// Restart stops then starts the spooler service - Windows' Service Control
// Manager has no atomic "restart" of its own, so this is the same two-step
// sequence a technician would perform by hand.
func Restart() error {
	if err := Stop(); err != nil {
		return err
	}
	return Start()
}

// Status reports the spooler service's current state as one of "running",
// "stopped", or "pending" (StartPending/StopPending/PausePending/
// ContinuePending/Paused all collapse to this - the frontend only ever needs
// "still settling" to show its own transitional color, not which specific
// pending state that is). Returned as a plain string rather than svc.State
// so nothing outside this package needs to import golang.org/x/sys/windows/svc
// just to read a status.
func Status() (string, error) {
	m, s, err := open()
	if err != nil {
		return "", err
	}
	defer m.Disconnect()
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
