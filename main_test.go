package main

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps the OS keychain for an in-memory mock, so no test in this
// package can ever read or overwrite a real Cloud Sync secret (loadSettings
// can now write one - see seedGuestCloudSync).
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
