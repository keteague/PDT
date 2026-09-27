package main

import "time"

// Frequencies for Settings > About's automatic update checks (PDT and 7-Zip
// each pick their own). Stored in settings.json under exactly these strings,
// and shown as-is in the frontend's combobox (UPDATE_FREQUENCIES in main.js
// must list the same values in the same order).
const (
	updateFreqOnStartup = "On Startup"
	updateFreqDaily     = "Daily"
	updateFreqWeekly    = "Weekly"
	updateFreqMonthly   = "Monthly"
	updateFreqQuarterly = "Quarterly"
	updateFreqYearly    = "Yearly"

	// defaultUpdateFrequency is what a fresh install (or a settings.json that
	// predates these fields) checks at. Not "On Startup": that would hit
	// GitHub and 7-zip.org on every single launch, which a technician
	// opening PDT several times a day never needs.
	defaultUpdateFrequency = updateFreqDaily
)

// normalizeUpdateFrequency maps anything that isn't one of the known
// frequencies (blank - an old settings.json - or unrecognized) to
// defaultUpdateFrequency, never an error - the same recover-to-default rule
// normalizeLogLevel applies to LogLevel.
func normalizeUpdateFrequency(freq string) string {
	switch freq {
	case updateFreqOnStartup, updateFreqDaily, updateFreqWeekly, updateFreqMonthly, updateFreqQuarterly, updateFreqYearly:
		return freq
	}
	return defaultUpdateFrequency
}

// updateCheckDue reports whether a check at frequency freq is due at now,
// given when the last successful one happened (zero if never). Monthly/
// Quarterly/Yearly are calendar intervals (a check on Jan 15 is next due on
// Feb 15), not fixed day counts. A last-check time in the future - the clock
// was set back since - counts as due rather than suppressing checks until
// that future date arrives.
func updateCheckDue(freq string, last, now time.Time) bool {
	freq = normalizeUpdateFrequency(freq)
	if freq == updateFreqOnStartup || last.IsZero() || last.After(now) {
		return true
	}
	var next time.Time
	switch freq {
	case updateFreqDaily:
		next = last.AddDate(0, 0, 1)
	case updateFreqWeekly:
		next = last.AddDate(0, 0, 7)
	case updateFreqMonthly:
		next = last.AddDate(0, 1, 0)
	case updateFreqQuarterly:
		next = last.AddDate(0, 3, 0)
	case updateFreqYearly:
		next = last.AddDate(1, 0, 0)
	}
	return !now.Before(next)
}

// dueUpdateChecks decides which automatic update checks should run now:
// each product's own checkbox and frequency, against when it last checked
// successfully.
func dueUpdateChecks(s Settings, st updateCheckState, now time.Time) (pdt, sevenZip bool) {
	pdt = !s.PDTUpdateCheckDisabled && updateCheckDue(s.PDTUpdateFrequency, st.PDT, now)
	sevenZip = !s.SevenZipUpdateCheckDisabled && updateCheckDue(s.SevenZipUpdateFrequency, st.SevenZip, now)
	return pdt, sevenZip
}
