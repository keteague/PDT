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
	Name         string
	IP           string
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
