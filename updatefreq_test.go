package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeUpdateFrequency(t *testing.T) {
	for _, f := range []string{"On Startup", "Daily", "Weekly", "Monthly", "Quarterly", "Yearly"} {
		if got := normalizeUpdateFrequency(f); got != f {
			t.Errorf("normalizeUpdateFrequency(%q) = %q, want it unchanged", f, got)
		}
	}
	for _, bad := range []string{"", "daily", "Hourly", "Montly"} {
		if got := normalizeUpdateFrequency(bad); got != defaultUpdateFrequency {
			t.Errorf("normalizeUpdateFrequency(%q) = %q, want default %q", bad, got, defaultUpdateFrequency)
		}
	}
}

func TestUpdateCheckDue(t *testing.T) {
	now := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	ago := func(y, m, d int) time.Time { return now.AddDate(-y, -m, -d) }

	cases := []struct {
		name string
		freq string
		last time.Time
		want bool
	}{
		{"never checked", "Yearly", time.Time{}, true},
		{"on startup, checked a second ago", "On Startup", now.Add(-time.Second), true},

		{"daily, 23h ago", "Daily", now.Add(-23 * time.Hour), false},
		{"daily, exactly 24h ago", "Daily", now.Add(-24 * time.Hour), true},

		{"weekly, 6 days ago", "Weekly", ago(0, 0, 6), false},
		{"weekly, 7 days ago", "Weekly", ago(0, 0, 7), true},

		// Calendar months: Aug 26 -> Sep 26 is exactly one month.
		{"monthly, 1 month less a day", "Monthly", ago(0, 1, -1), false},
		{"monthly, 1 month ago", "Monthly", ago(0, 1, 0), true},

		{"quarterly, 3 months less a day", "Quarterly", ago(0, 3, -1), false},
		{"quarterly, 3 months ago", "Quarterly", ago(0, 3, 0), true},

		{"yearly, 1 year less a day", "Yearly", ago(1, 0, -1), false},
		{"yearly, 1 year ago", "Yearly", ago(1, 0, 0), true},

		// Clock set back since the last check: don't wait for that future date.
		{"last check in the future", "Yearly", now.Add(48 * time.Hour), true},

		// An unrecognized frequency behaves as the default (Daily).
		{"unknown frequency, 1h ago", "Hourly", now.Add(-time.Hour), false},
		{"unknown frequency, 25h ago", "Hourly", now.Add(-25 * time.Hour), true},
	}
	for _, c := range cases {
		if got := updateCheckDue(c.freq, c.last, now); got != c.want {
			t.Errorf("%s: updateCheckDue(%q, %v) = %v, want %v", c.name, c.freq, c.last.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestDueUpdateChecks_EachProductIndependent(t *testing.T) {
	now := time.Date(2026, time.September, 26, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-25 * time.Hour)
	justNow := now.Add(-time.Minute)

	// PDT is due (daily, last yesterday); 7-Zip is monthly and was just checked.
	s := Settings{PDTUpdateFrequency: "Daily", SevenZipUpdateFrequency: "Monthly"}
	pdt, sz := dueUpdateChecks(s, updateCheckState{PDT: yesterday, SevenZip: justNow}, now)
	if !pdt || sz {
		t.Errorf("dueUpdateChecks = pdt %v, 7-Zip %v; want true, false", pdt, sz)
	}

	// Each checkbox off suppresses only its own product, even when due.
	s = Settings{PDTUpdateCheckDisabled: true, PDTUpdateFrequency: "On Startup", SevenZipUpdateFrequency: "On Startup"}
	pdt, sz = dueUpdateChecks(s, updateCheckState{}, now)
	if pdt || !sz {
		t.Errorf("PDT disabled: dueUpdateChecks = pdt %v, 7-Zip %v; want false, true", pdt, sz)
	}
	s = Settings{SevenZipUpdateCheckDisabled: true, PDTUpdateFrequency: "On Startup", SevenZipUpdateFrequency: "On Startup"}
	pdt, sz = dueUpdateChecks(s, updateCheckState{}, now)
	if !pdt || sz {
		t.Errorf("7-Zip disabled: dueUpdateChecks = pdt %v, 7-Zip %v; want true, false", pdt, sz)
	}
}

// isolateUserConfigDir points os.UserConfigDir at a temp folder on every
// platform (%AppData% on Windows, $HOME/Library/Application Support on macOS,
// $XDG_CONFIG_HOME elsewhere) so a test never touches the real settings.
func isolateUserConfigDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("AppData", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// A settings.json written before these fields existed comes up with both
// checks enabled at the default frequency.
func TestLoadSettings_UpdateChecksDefaultOn(t *testing.T) {
	dir := isolateUserConfigDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "PDT"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PDT", "settings.json"), []byte(`{"logLevel":"Debug"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := loadSettings()
	if s.PDTUpdateCheckDisabled || s.SevenZipUpdateCheckDisabled {
		t.Errorf("update checks disabled (PDT %v, 7-Zip %v), want both enabled by default", s.PDTUpdateCheckDisabled, s.SevenZipUpdateCheckDisabled)
	}
	if s.PDTUpdateFrequency != defaultUpdateFrequency || s.SevenZipUpdateFrequency != defaultUpdateFrequency {
		t.Errorf("frequencies = %q/%q, want %q for both", s.PDTUpdateFrequency, s.SevenZipUpdateFrequency, defaultUpdateFrequency)
	}
}

func TestLoadSettings_UpdateChecksRoundTrip(t *testing.T) {
	dir := isolateUserConfigDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "PDT"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := Settings{
		PDTUpdateCheckDisabled:      true,
		PDTUpdateFrequency:          "Quarterly",
		SevenZipUpdateCheckDisabled: false,
		SevenZipUpdateFrequency:     "On Startup",
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "PDT", "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadSettings()
	if got.PDTUpdateCheckDisabled != true || got.PDTUpdateFrequency != "Quarterly" ||
		got.SevenZipUpdateCheckDisabled != false || got.SevenZipUpdateFrequency != "On Startup" {
		t.Errorf("loadSettings = PDT %v/%q, 7-Zip %v/%q; want PDT true/Quarterly, 7-Zip false/On Startup",
			got.PDTUpdateCheckDisabled, got.PDTUpdateFrequency, got.SevenZipUpdateCheckDisabled, got.SevenZipUpdateFrequency)
	}
}

func TestUpdateCheckState_RoundTripAndMissingFile(t *testing.T) {
	isolateUserConfigDir(t)
	if st := loadUpdateCheckState(); !st.PDT.IsZero() || !st.SevenZip.IsZero() {
		t.Errorf("state with no file = %+v, want zero values", st)
	}

	pdt := time.Date(2026, time.September, 26, 12, 30, 0, 0, time.UTC)
	if err := saveUpdateCheckState(updateCheckState{PDT: pdt}); err != nil {
		t.Fatal(err)
	}
	got := loadUpdateCheckState()
	if !got.PDT.Equal(pdt) || !got.SevenZip.IsZero() {
		t.Errorf("state = %+v, want PDT %v and zero 7-Zip", got, pdt)
	}
}
