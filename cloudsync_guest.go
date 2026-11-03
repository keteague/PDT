package main

import "PDT/internal/cloudsync"

// The public, read-only guest key for the shared driver repository - the
// same pair the README's "Shared driver repository (guest access)" section
// publishes, so embedding it here exposes nothing new. Read-only by
// Cloudflare's own permissions; PDT detects that (cloudCanWrite) and offers
// downloads only.
const (
	guestCloudAccessKeyID = "6eb7d2aebdcb5111124420cb9f7b9957"
	guestCloudSecretKey   = "2215309857a8750d38b549fb48fb4816f1dfa2fddf21d7c3f22b87eb14cd07f8"
)

// guestCloudSyncSeeded reports that loadSettings just preconfigured the
// guest account, for startup to log and persist once.
var guestCloudSyncSeeded bool

// seedGuestCloudSync preconfigures Cloud Sync with the guest key on a brand
// new installed copy only (Ken, 2026-11-03). loadSettings calls it only when
// no settings.json exists in either the current or the legacy location -
// any existing installation, whatever its Cloud Sync state (including none
// at all), is never touched. Also skipped when:
//   - this is a portable copy: a flash drive gets plugged into client PCs,
//     and writing a key into a client PC's credential store is not wanted;
//   - the OS keychain already holds a secret - a technician's own account
//     from an earlier install (e.g. reinstalled after deleting settings) -
//     so a real key is never replaced, and never paired with the guest ID.
func seedGuestCloudSync(s *Settings) {
	if portableExeDir() != "" || cloudsync.HasSecretKey() {
		return
	}
	if err := cloudsync.SaveSecretKey(guestCloudSecretKey); err != nil {
		return
	}
	s.CloudSync.AccessKeyID = guestCloudAccessKeyID
	guestCloudSyncSeeded = true
}
