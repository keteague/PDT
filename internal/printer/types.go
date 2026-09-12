package printer

import (
	"context"
	"errors"
	"strings"
)

// PrinterRow is one deployable printer definition. There is no BindNulPort
// field (the NUL-port workaround is now automatic - see requiresNulPortWorkaround
// in service.go) and no Select field (that's frontend/JSON-only UI state, never
// part of the data a deploy actually needs).
type PrinterRow struct {
	Name string
	IP   string
	// LPDQueueName is the optional queue-name segment of a macOS deploy's LPD
	// device URI (lpd://<ip>/<LPDQueueName> - see
	// internal/printer/darwin/deploy_darwin.go's own Deploy). Most
	// manufacturers' MFDs ignore it and respond to any/no queue name; HP
	// ("raw") and Xerox ("lp") are the two known exceptions, which the
	// frontend auto-fills for those manufacturers (still overridable per
	// row) - see frontend/src/main.js's defaultLpdQueueFor. Carried on
	// Windows too, and round-tripped through Open/Save Configuration
	// (config.SavedRow) and CSV, even though Windows' own Deploy never reads
	// it - Standard TCP/IP ports have no LPD-queue concept at all - purely so
	// a config saved on Windows already has the right value the moment it's
	// opened on a Mac, without a technician needing to redo this per row.
	LPDQueueName string
	Manufacturer string
	Model        string
	Driver       string
	SNMP         bool
	// SNMPCommunity is the community string used when SNMP is true and a new
	// port is actually created. The grid's own SNMP field is this string
	// directly (blank means SNMP disabled, non-blank both enables it and
	// supplies the community); SNMP itself stays a separate bool here
	// because AddStandardTcpIpPort's own signature already took one before
	// this field existed, and PrinterRow is a public boundary type not worth
	// reshaping over it. Empty means "public" (AddStandardTcpIpPort's own
	// default) even when SNMP is true.
	SNMPCommunity            string
	Mono                     bool
	OneSided                 bool
	UseExistingPort          bool
	AdvancedPrintingFeatures bool
	// DevModeFile is the bare filename (not a full path) of a captured raw
	// DEVMODE under the Configs folder, e.g. "18455-1-Copy Room.bin" - a
	// pointer, never the DEVMODE bytes themselves (which never travel through
	// JSON at all - see ResolveDevModePath). Empty means no DEVMODE was
	// explicitly captured/browsed for this row; Deploy still checks the
	// Configs folder by convention as a fallback in that case.
	DevModeFile string
}

// NulPortName is the local Windows port every NUL-bound printer is created
// against, either permanently (the user typed "NUL"/"NUL:" as the IP) or
// temporarily during the HP-Universal-Print-Driver workaround (see service.go).
const NulPortName = "NUL:"

// ErrMissingIP is returned by NormalizeIP when raw is empty. Empty IP is
// always invalid now: the old "blank IP + BindNulPort checked" placeholder-row
// case is expressed by typing "NUL" as the IP instead, so there is no longer
// any valid reason for IP to be blank.
var ErrMissingIP = errors.New("row is missing required IP (use \"NUL\" to bind permanently to the local NUL: port)")

// NormalizeIP trims raw and reports whether it's the NUL-port sentinel.
// "" always errors. "nul" or "nul:" (any case, trimmed) normalizes to
// (NulPortName, true, nil). Anything else is returned trimmed, unvalidated as
// an actual IP address or hostname - same as the original tool, which never
// validated IP format either.
func NormalizeIP(raw string) (value string, isNul bool, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, ErrMissingIP
	}
	switch strings.ToLower(trimmed) {
	case "nul", "nul:":
		return NulPortName, true, nil
	default:
		return trimmed, false, nil
	}
}

// DeployRequest is one row's deployment job, plus the run-wide settings that
// affect it.
type DeployRequest struct {
	Row            PrinterRow
	SalesChainID   string
	PortNamePrefix string
}

// DeployResult is the outcome of deploying one row, including its progress
// log lines (so the UI can show what happened even when Err is nil).
type DeployResult struct {
	RowName string
	Log     []string
	Err     error
}

// Confirm is how the deployer asks the UI a yes/no question mid-deploy (e.g.
// "this printer already exists, apply these changes?" or "install this
// different driver version?"). Returns the user's answer.
type Confirm func(ctx context.Context, title, message string) (bool, error)

// Deployer is implemented per-platform (windows for real, darwin as a stub
// for now) and is the only thing app.go depends on directly.
type Deployer interface {
	Deploy(ctx context.Context, req DeployRequest, confirm Confirm) DeployResult
}

// BatchPreparer is an optional Deployer extension, checked via a type
// assertion in DeployAllWithProgress before the first row's own Deploy()
// call. macOS's own Deployer implements this to collapse every batchable
// row's own privileged operations (driver install, queue create) into a
// single elevated `do shell script ... with administrator privileges` call
// up front - confirmed live, repeatedly, that each such call always shows
// its own fresh native prompt with no caching between separate calls, so
// this is the only way to get down to one prompt for a whole multi-row
// deploy run rather than one (or several) per row. A row PrepareBatch
// couldn't handle (a different manufacturer, or the guess-based fallback
// path with no catalog entry) is simply left alone - its own Deploy() call
// falls back to doing its own privileged work exactly as before, so
// PrepareBatch failing or only partially applying is never a hard stop for
// the whole run.
//
// The real trade-off PrepareBatch accepts: every batched row's success or
// failure becomes known only once the single combined call returns, not
// streamed in as each row would otherwise finish - a deliberate choice, not
// an oversight, given the alternative is a fresh native password prompt for
// every row needing its own privileged step.
type BatchPreparer interface {
	PrepareBatch(ctx context.Context, reqs []DeployRequest, confirm Confirm)
}
