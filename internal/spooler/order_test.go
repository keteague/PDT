package spooler

import (
	"errors"
	"reflect"
	"testing"
)

func fakeGraph(g map[string][]string) func(string) ([]string, error) {
	return func(name string) ([]string, error) { return g[name], nil }
}

// indexOf fails the test if name isn't in order.
func indexOf(t *testing.T, order []string, name string) int {
	t.Helper()
	for i, n := range order {
		if n == name {
			return i
		}
	}
	t.Fatalf("%q missing from stop order %v", name, order)
	return -1
}

func TestStopOrder_NoDependents(t *testing.T) {
	got, err := stopOrder("Spooler", fakeGraph(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("stopOrder = %v, want empty", got)
	}
}

// Spooler <- Fax <- FaxHelper: the indirect dependent has to stop first.
func TestStopOrder_ChainStopsDeepestFirst(t *testing.T) {
	got, err := stopOrder("Spooler", fakeGraph(map[string][]string{
		"Spooler": {"Fax"},
		"Fax":     {"FaxHelper"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"FaxHelper", "Fax"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stopOrder = %v, want %v", got, want)
	}
}

// A service depending on two different Spooler dependents (a diamond)
// appears once, before both of them.
func TestStopOrder_DiamondListedOnceBeforeEveryParent(t *testing.T) {
	got, err := stopOrder("Spooler", fakeGraph(map[string][]string{
		"Spooler": {"A", "B"},
		"A":       {"Shared"},
		"B":       {"Shared"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("stopOrder = %v, want 3 distinct services", got)
	}
	shared := indexOf(t, got, "Shared")
	if shared > indexOf(t, got, "A") || shared > indexOf(t, got, "B") {
		t.Errorf("stopOrder = %v, want Shared before both A and B", got)
	}
}

// EnumDependentServices can report indirect dependents directly under the
// root as well, in either order - both must still come out safe.
func TestStopOrder_IndirectAlsoListedUnderRoot(t *testing.T) {
	for _, rootDeps := range [][]string{{"Fax", "FaxHelper"}, {"FaxHelper", "Fax"}} {
		got, err := stopOrder("Spooler", fakeGraph(map[string][]string{
			"Spooler": rootDeps,
			"Fax":     {"FaxHelper"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"FaxHelper", "Fax"}; !reflect.DeepEqual(got, want) {
			t.Errorf("root deps %v: stopOrder = %v, want %v", rootDeps, got, want)
		}
	}
}

// A (misconfigured) cycle back to the root or between dependents must
// terminate rather than recurse forever, and never lists the root itself.
func TestStopOrder_CycleTerminates(t *testing.T) {
	got, err := stopOrder("Spooler", fakeGraph(map[string][]string{
		"Spooler": {"A"},
		"A":       {"B"},
		"B":       {"A", "Spooler"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"B", "A"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stopOrder = %v, want %v", got, want)
	}
}

func TestStopOrder_PropagatesListingError(t *testing.T) {
	boom := errors.New("access denied")
	_, err := stopOrder("Spooler", func(name string) ([]string, error) {
		if name == "Fax" {
			return nil, boom
		}
		return []string{"Fax"}, nil
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap %v", err, boom)
	}
}
