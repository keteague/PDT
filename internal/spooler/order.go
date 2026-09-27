package spooler

import "fmt"

// stopOrder returns every service that transitively depends on root (root
// itself excluded), ordered so that each service comes after everything that
// depends on it - i.e. a safe order to stop them in before stopping root.
// Starting them again afterward is the same list reversed.
//
// dependents reports a service's direct dependents (the Service Control
// Manager's EnumDependentServices may already include indirect ones too -
// harmless here, since the visited set dedups and a post-order walk still
// places every service after all of its own dependents either way).
//
// Kept free of any real SCM calls so the ordering itself is unit-testable
// against a fake dependency graph (see order_test.go).
func stopOrder(root string, dependents func(name string) ([]string, error)) ([]string, error) {
	var order []string
	visited := map[string]bool{root: true}
	var visit func(name string) error
	visit = func(name string) error {
		deps, err := dependents(name)
		if err != nil {
			return fmt.Errorf("listing services that depend on %q: %w", name, err)
		}
		for _, d := range deps {
			if visited[d] {
				continue
			}
			visited[d] = true
			if err := visit(d); err != nil {
				return err
			}
			order = append(order, d)
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return order, nil
}
