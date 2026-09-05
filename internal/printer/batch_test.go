package printer

import (
	"context"
	"errors"
	"testing"
)

type fakeDeployer struct {
	failNames map[string]bool
	deployed  []string
}

func (f *fakeDeployer) Deploy(ctx context.Context, req DeployRequest, confirm Confirm) DeployResult {
	f.deployed = append(f.deployed, req.Row.Name)
	if f.failNames[req.Row.Name] {
		return DeployResult{RowName: req.Row.Name, Err: errors.New("boom")}
	}
	return DeployResult{RowName: req.Row.Name}
}

func TestDeployAllContinuesPastFatalRowError(t *testing.T) {
	fd := &fakeDeployer{failNames: map[string]bool{"b": true}}
	reqs := []DeployRequest{
		{Row: PrinterRow{Name: "a"}},
		{Row: PrinterRow{Name: "b"}},
		{Row: PrinterRow{Name: "c"}},
	}

	results := DeployAll(context.Background(), fd, reqs, nil)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if !equalStrings(fd.deployed, []string{"a", "b", "c"}) {
		t.Fatalf("deployed order = %v, want every row attempted in order", fd.deployed)
	}
	if results[0].Err != nil || results[2].Err != nil {
		t.Fatalf("rows a and c should have succeeded: %v / %v", results[0].Err, results[2].Err)
	}
	if results[1].Err == nil {
		t.Fatalf("row b should have failed")
	}
}

func TestDeployAllStopsOnCanceledContext(t *testing.T) {
	fd := &fakeDeployer{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reqs := []DeployRequest{{Row: PrinterRow{Name: "a"}}, {Row: PrinterRow{Name: "b"}}}
	results := DeployAll(ctx, fd, reqs, nil)

	if len(results) != 0 {
		t.Fatalf("got %d results, want 0 for an already-canceled context", len(results))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
