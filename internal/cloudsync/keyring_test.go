package cloudsync

import (
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain switches go-keyring into its own in-memory mock for every test in
// this package - without this, these tests would read/write the real OS
// keychain on whatever machine runs `go test`, which is both slow and not
// something a test suite should be touching.
func TestMain(m *testing.M) {
	keyring.MockInit()
	m.Run()
}

func TestSecretKey_RoundTrip(t *testing.T) {
	if HasSecretKey() {
		t.Fatal("expected no secret stored before this test sets one")
	}
	if err := SaveSecretKey("r2-secret-value"); err != nil {
		t.Fatal(err)
	}
	if !HasSecretKey() {
		t.Error("expected HasSecretKey() to be true after SaveSecretKey")
	}
	got, err := LoadSecretKey()
	if err != nil {
		t.Fatal(err)
	}
	if got != "r2-secret-value" {
		t.Errorf("LoadSecretKey() = %q, want %q", got, "r2-secret-value")
	}

	if err := DeleteSecretKey(); err != nil {
		t.Fatal(err)
	}
	if HasSecretKey() {
		t.Error("expected HasSecretKey() to be false after DeleteSecretKey")
	}
}

func TestLoadSecretKey_EmptyNotErrorWhenNothingStored(t *testing.T) {
	_ = DeleteSecretKey()
	got, err := LoadSecretKey()
	if err != nil {
		t.Fatalf("LoadSecretKey() with nothing stored returned an error: %v", err)
	}
	if got != "" {
		t.Errorf("LoadSecretKey() with nothing stored = %q, want \"\"", got)
	}
}

func TestDeleteSecretKey_NoOpWhenNothingStored(t *testing.T) {
	_ = DeleteSecretKey()
	if err := DeleteSecretKey(); err != nil {
		t.Errorf("DeleteSecretKey() with nothing stored returned %v, want nil", err)
	}
}
