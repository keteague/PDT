package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"PDT/internal/drvsign"

	"golang.org/x/sys/windows"
)

// TestEnsurePublishersTrusted_Live runs the whole flow against a real,
// signed driver package when PDT_TEST_INF points at its .inf (with its
// .cat beside it) - e.g. Kyocera's 64bit\OEMSETUP.INF. It never touches the
// machine's real Trusted Publishers: it uses a throwaway store under the
// current user, deleted afterwards.
func TestEnsurePublishersTrusted_Live(t *testing.T) {
	inf := os.Getenv("PDT_TEST_INF")
	if inf == "" {
		t.Skip("PDT_TEST_INF not set")
	}
	loc := certStoreLocation{name: fmt.Sprintf("PDTTest%d", time.Now().UnixNano()), flags: windows.CERT_SYSTEM_STORE_CURRENT_USER}
	t.Cleanup(func() { deleteTestStore(loc) })
	resetRunCache := func() {
		trustedThisRunMu.Lock()
		trustedThisRun = map[string]bool{}
		trustedThisRunMu.Unlock()
	}
	resetRunCache()

	first, err := ensurePublishersTrustedIn(inf, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 || !first[0].Added {
		t.Fatalf("first run = %+v, want the publisher added to the empty store", first)
	}
	t.Logf("added %q (%s)", first[0].Name, first[0].Thumbprint)

	second, err := ensurePublishersTrustedIn(inf, loc)
	if err != nil || len(second) == 0 || second[0].Added {
		t.Fatalf("second run = %+v, %v; want already trusted (remembered this run)", second, err)
	}

	resetRunCache()
	third, err := ensurePublishersTrustedIn(inf, loc)
	if err != nil || len(third) == 0 || third[0].Added {
		t.Fatalf("third run = %+v, %v; want already trusted (found in the store)", third, err)
	}
}

// A catalog whose signature doesn't verify must never get its signer
// trusted - copy the real package's .inf and .cat, corrupt one byte of the
// catalog's signed content, and confirm nothing is added.
func TestEnsurePublishersTrusted_TamperedCatalogRefused(t *testing.T) {
	inf := os.Getenv("PDT_TEST_INF")
	if inf == "" {
		t.Skip("PDT_TEST_INF not set")
	}
	cats, err := drvsign.CatalogFiles(inf)
	if err != nil || len(cats) == 0 {
		t.Fatalf("no catalog for %s: %v", inf, err)
	}
	dir := t.TempDir()
	infData, _ := os.ReadFile(inf)
	catData, _ := os.ReadFile(cats[0])
	catData[len(catData)/3] ^= 0xFF // inside the hashed catalog content
	os.WriteFile(filepath.Join(dir, filepath.Base(inf)), infData, 0o644)
	os.WriteFile(filepath.Join(dir, filepath.Base(cats[0])), catData, 0o644)

	loc := certStoreLocation{name: fmt.Sprintf("PDTTest%d", time.Now().UnixNano()), flags: windows.CERT_SYSTEM_STORE_CURRENT_USER}
	t.Cleanup(func() { deleteTestStore(loc) })
	trustedThisRunMu.Lock()
	trustedThisRun = map[string]bool{}
	trustedThisRunMu.Unlock()

	got, err := ensurePublishersTrustedIn(filepath.Join(dir, filepath.Base(inf)), loc)
	if err == nil {
		t.Fatalf("expected a signature error, got %+v", got)
	}
	for _, p := range got {
		if p.Added {
			t.Fatalf("a tampered catalog's signer was trusted: %+v", p)
		}
	}
	t.Logf("refused: %v", err)
}

func deleteTestStore(loc certStoreLocation) {
	name, _ := windows.UTF16PtrFromString(loc.name)
	_, _ = windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0, loc.flags|windows.CERT_STORE_DELETE_FLAG, uintptr(unsafe.Pointer(name)))
}
