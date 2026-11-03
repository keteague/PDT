package cloudsync

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestProbeWriteAccess_Live runs ProbeWriteAccess against a real R2 bucket.
// Skipped unless PDT_R2_ACCESS_KEY and PDT_R2_SECRET_KEY are set (endpoint
// and bucket default to the shared PDT bucket); set PDT_R2_EXPECT_WRITE to
// "true" or "false" to assert the expected answer for that key.
func TestProbeWriteAccess_Live(t *testing.T) {
	ak, sk := os.Getenv("PDT_R2_ACCESS_KEY"), os.Getenv("PDT_R2_SECRET_KEY")
	if ak == "" || sk == "" {
		t.Skip("PDT_R2_ACCESS_KEY / PDT_R2_SECRET_KEY not set")
	}
	if sk == "keychain" { // use the secret PDT itself saved in the OS keychain
		var err error
		if sk, err = LoadSecretKey(); err != nil || sk == "" {
			t.Skipf("no secret in the keychain: %v", err)
		}
	}
	endpoint := os.Getenv("PDT_R2_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://60a85894a285594161353b7731fa385f.r2.cloudflarestorage.com"
	}
	bucket := os.Getenv("PDT_R2_BUCKET")
	if bucket == "" {
		bucket = "pdt"
	}
	core, err := NewCore(Config{Endpoint: endpoint, Bucket: bucket, AccessKeyID: ak, SecretAccessKey: sk})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	canWrite, err := ProbeWriteAccess(ctx, core, bucket, "Drivers/")
	t.Logf("canWrite=%v err=%v", canWrite, err)
	if err != nil {
		t.Fatalf("probe was inconclusive: %v", err)
	}
	switch os.Getenv("PDT_R2_EXPECT_WRITE") {
	case "true":
		if !canWrite {
			t.Error("expected write access")
		}
	case "false":
		if canWrite {
			t.Error("expected read-only")
		}
	}

	// Listing must still work for either kind of key, and the aborted probe
	// must not have left anything visible behind.
	remote, err := listRemote(ctx, core, bucket, "Drivers/")
	if err != nil {
		t.Fatalf("listing failed: %v", err)
	}
	if _, ok := remote[probeKeyName]; ok {
		t.Error("probe key shows up in the listing")
	}
	t.Logf("listed %d objects", len(remote))
}
