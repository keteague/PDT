package printer

import (
	"fmt"
	"time"
)

// clockNow is a var (not a direct time.Now call) so tests can override it to
// get deterministic timestamps.
var clockNow = time.Now

// Logger accumulates one row's progress lines, each timestamped and tagged
// with a level - [INFO]/[OK]/[WARN]/[ERR] - per spec: every log line needs a
// date/time stamp and a clear indicator of what kind of line it is, so the UI
// can show it directly with no further formatting.
type Logger struct {
	lines []string
}

func (l *Logger) log(level, format string, args ...any) {
	l.lines = append(l.lines, clockNow().Format("2006-01-02 15:04:05")+" ["+level+"] "+fmt.Sprintf(format, args...))
}

// Info records a neutral progress note - nothing changed, nothing failed.
func (l *Logger) Info(format string, args ...any) { l.log("INFO", format, args...) }

// OK records a step that was attempted and succeeded.
func (l *Logger) OK(format string, args ...any) { l.log("OK", format, args...) }

// Warn records a non-fatal problem: a print-object *setting* (driver version
// pin declined, duplex/color, APF) failed or didn't take, but the printer
// object itself is still fine and deployment continues.
func (l *Logger) Warn(format string, args ...any) { l.log("WARN", format, args...) }

// Err records the fatal problem that ended this row's deployment - the
// printer object itself could not be created/updated.
func (l *Logger) Err(format string, args ...any) { l.log("ERR", format, args...) }

// Lines returns every line recorded so far, for DeployResult.Log.
func (l *Logger) Lines() []string { return l.lines }
