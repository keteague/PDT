package openprinting

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestLive_ListingParses checks ParseListing against the real site. Opt-in
// (PDT_LIVE_TESTS=1) so the normal test run never touches the network.
func TestLive_ListingParses(t *testing.T) {
	if os.Getenv("PDT_LIVE_TESTS") == "" {
		t.Skip("set PDT_LIVE_TESTS=1 to hit openprinting.org")
	}
	client := &http.Client{Timeout: time.Minute}
	base, _ := url.Parse(DefaultBaseURL)
	top, err := list(context.Background(), client, base)
	if err != nil {
		t.Fatal(err)
	}
	dirs := 0
	for _, e := range top {
		if e.IsDir {
			dirs++
		}
	}
	if dirs < 10 {
		t.Fatalf("only %d manufacturer folders parsed from the live index: %+v", dirs, top)
	}
	for _, m := range []string{"Kyocera", "HP", "Konica Minolta"} {
		var remote string
		for _, e := range top {
			if e.IsDir && normName(e.Name) == normName(m) {
				remote = e.Name
			}
		}
		if remote == "" {
			t.Errorf("no remote folder for %s", m)
			continue
		}
		jobs, err := walk(context.Background(), client, base.JoinPath(remote+"/"), remote, t.TempDir(), nil)
		if err != nil {
			t.Errorf("%s: %v", m, err)
			continue
		}
		t.Logf("%s (%s): %d PPDs, first: %+v", m, remote, len(jobs), firstJob(jobs))
		if len(jobs) == 0 {
			t.Errorf("%s: no PPDs found", m)
		}
	}
}

func firstJob(jobs []job) any {
	if len(jobs) == 0 {
		return nil
	}
	return jobs[0]
}
