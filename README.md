# PDT (Printer Deployment Tool)

A Wails (Go + web frontend) rewrite of `Create-Printers.ps1`, the original PowerShell/WinForms
tool for bulk-deploying network printers (Standard TCP/IP ports, vendor drivers, print
configuration) from a CSV or saved JSON list of rows. This rewrite exists to get off the
PowerShell/WinForms DataGridView layer entirely while keeping every behavior the original tool's
real-world use already proved out - the Win32 mechanics below are ported (not reinvented) from
that tool's own hard-won findings, cross-checked against Microsoft's documentation and, wherever
that documentation didn't cover it, verified directly against this machine's real print spooler.

## Status

- **Phase 1 - data layer** (`internal/config`, `internal/driver`, `internal/printer` types): CSV/JSON
  import-export, the driver catalog (multi-version/multi-arch scanning of a `Drivers/<Manufacturer>/...`
  tree, `Archive` subfolders excluded), driver-name/version resolution, and the platform-independent
  `PrinterRow`/`DeployRequest`/`DeployResult` shapes every phase builds on. Done.
- **Phase 2 - Win32 bindings** (`internal/printer/windows`): hand-written syscall bindings for every
  winspool.drv/setupapi.dll operation the tool needs - `golang.org/x/sys/windows` has no
  winspool.drv coverage at all. Done; see "Windows bindings" below.
- **Phase 3 - orchestration** (`internal/printer/windows/deploy_windows.go`, `internal/printer/batch.go`):
  the actual per-row Deploy sequence (resolve driver -> port -> driver install -> create/update
  printer -> conditional NUL:-to-real-port rebind -> print config -> APF) wired on top of phases 1-2,
  plus the confirmation-dialog flows for driver-version changes and printer updates. Done; see
  "Deploy sequence" below.
- **Not started**: the Wails frontend itself (`app.go`/`frontend/` are still the unmodified project
  template) and macOS/Linux support (`printer.Deployer` is implemented for Windows only; there is no
  `darwin`/other-OS stub yet).

## Windows bindings (`internal/printer/windows`)

`golang.org/x/sys/windows` has zero winspool.drv coverage, so every binding here is hand-written
against Microsoft's Win32 documentation, and - for the handful of things that documentation doesn't
cover at all - verified directly against this machine's real print spooler (see `cmd/pdtdebug`
below).

| File | What it does |
|---|---|
| `printer_windows.go` | `OpenPrinter`/`GetInfo2`/`SetInfo2`/`CreatePrinter`/`DeletePrinterByName`/`EnumLocalPrinterNames` - the PRINTER_INFO_2 read/write core everything else builds on. Always requests `PRINTER_ALL_ACCESS` explicitly (a `NULL` `PRINTER_DEFAULTS` grants a default access level too low for `SetPrinter` to succeed - the original tool's own early bug). |
| `apf_windows.go` | The printer Advanced tab's "Enable advanced printing features" toggle: `PRINTER_ATTRIBUTE_RAW_ONLY`, **inverted** (enabling APF clears the bit) - confirmed against Microsoft's docs after an earlier wrong guess (`PRINTER_ATTRIBUTE_ENABLE_DEVQ`) in the original tool. |
| `port_windows.go` | `NUL:` port creation via `XcvData "AddPort"` against the "Local Port" monitor. The obvious-looking `AddPortEx` API was tried first and rejected - it's documented as NT4-legacy and returns `ERROR_INVALID_PARAMETER` against the modern Local Port monitor in practice; `XcvData` is the real, current mechanism (and what Standard TCP/IP port creation below also needs). |
| `tcpport_windows.go` | Standard TCP/IP port creation via `XcvData "AddPort"`/`PORT_DATA_1` against the "Standard TCP/IP Port" monitor - the one piece with no PowerShell-cmdlet internals to port from at all (the original tool used the `Win32_TCPIPPrinterPort` WMI class, a completely different mechanism), built from Microsoft's TCPMON Xcv Commands documentation and verified end-to-end (see below). |
| `driverinstall_windows.go` | Driver installation via `SetupCopyOEMInfW` (stage into the driver store) + `InstallPrinterDriverFromPackageW` (register with the spooler). The latter's real export turned out to live in `winspool.drv` on this machine, not `spoolss.dll` as Microsoft's own docs list - found via the actual runtime error, not assumed. |
| `devmode_windows.go` | Duplex/color via `DocumentPropertiesW`/`DEVMODE`, replacing the original tool's unreliable `Set-PrintConfiguration` + PrintTicket-XML fallback entirely. **Known limitation, ported forward from the original tool**: Kyocera's driver won't reliably switch back from monochrome to color via DEVMODE (confirmed non-transient); logged as a `[WARN]`, never fatal. |
| `driverinfo_windows.go` | Reads an *installed* driver's real version straight from the registry (`...\Environments\<env>\Drivers\Version-3\<name>`, values `DriverDate`/`DriverVersion`) - confirmed on a real machine to hold the same version string a vendor `.inf`'s own `DriverVer=` line declares, unlike `Get-PrinterDriver`'s CIM-derived `Date`/`DriverVersion` properties, which the original tool found came back unusable regardless of how the driver was installed. |
| `portlookup_windows.go` | Finds an existing Standard TCP/IP port already targeting a given host, by reading every port's `HostName` value from the registry (`...\Monitors\Standard TCP/IP Port\Ports\<name>`) - the registry-level equivalent of the original tool's `Win32_TCPIPPrinterPort.HostAddress` WMI lookup. |
| `structs_windows.go` | Every Win32 struct used above (`DEVMODE`, `PRINTER_INFO_2`, `PORT_DATA_1`, `DELETE_PORT_DATA_1`, ...), laid out to match the real C structs byte-for-byte - see `structs_windows_test.go`'s `unsafe.Sizeof` assertions against the documented sizes (`DEVMODE`=220, `PRINTER_INFO_2`=136, `PORT_DATA_1`=964, `DELETE_PORT_DATA_1`=236). |

### `cmd/pdtdebug`

A throwaway CLI for verifying the bindings above against this machine's real print spooler -
mirroring how the driver-catalog logic was validated headlessly against real local files. It has no
role in the shipped app; delete it once the Wails UI exercises the same codepaths directly. Needs an
elevated terminal (every printer/port operation requires Administrator).

Notably: `deployrow <driversRoot> <manufacturer> <driverSelection> <ip> [nocleanup] [useexisting]`
runs the *entire* Phase 3 Deploy sequence end to end against a real local driver catalog and this
machine's real spooler for one throwaway printer, auto-confirming every prompt, then cleans up after
itself (unless `nocleanup` is given, to set up a same-row redeploy test). Run `pdtdebug` with no
arguments for the full command list.

## Deploy sequence (`internal/printer/windows/deploy_windows.go`)

`Deployer.Deploy` ports `Create-Printers.ps1`'s `Deploy-PrinterRow`, in the same order (port ->
driver -> printer object -> conditional rebind -> print config -> APF), with one deliberate
simplification versus the original: there is no `BindNulPort` checkbox. Instead:

- Typing `NUL` (or `NUL:`) as a row's IP permanently binds it to the local `NUL:` port (no network
  I/O at all) - for a placeholder/test row.
- Any row whose resolved driver is HP's Universal Print Driver family automatically gets the
  create-against-`NUL:`-then-rebind-to-the-real-port treatment with **no user toggle** -
  `printer.RequiresNulPortWorkaround` - since that HP driver family has been directly observed to
  take several minutes to create against a live TCP/IP port versus a few seconds against `NUL:`.

**Fatal vs. warning, per spec**: creating or updating the printer object itself (port resolution,
driver install, `CreatePrinter`/`SetInfo2`, the NUL:-to-real-port rebind) is the one thing that
matters most - failing any of those steps is fatal *for that row only* (`DeployResult.Err`, logged
`[ERR]`) and `printer.DeployAll` always continues to the next row regardless. Everything after the
object exists - duplex/color, advanced printing features - is best-effort: a failure there is a
`[WARN]`, never stops the row, and both are always attempted even if one already failed.

Every log line is timestamped and level-tagged (`internal/printer/log.go`'s `Logger`:
`[INFO]`/`[OK]`/`[WARN]`/`[ERR]`) so the UI can show a row's progress directly with no further
formatting.

### Driver version handling

- **Plain name** selection (the common case): silently upgrades an older installed driver to the
  newer local package; never silently downgrades.
- **An explicit version pin** (one of the driver catalog's `<name> (vVersion - date)` labels):
  honored upgrade *or* downgrade, but only after a confirmation dialog, since Windows shares one
  driver registration per name across every printer already using it. Declining leaves the
  currently-installed version in place (`[WARN]`, not fatal).

### Existing printer objects

If a printer with the row's exact name already exists, its current driver/port/comment are diffed
against what this row would set; a confirmation dialog lists exactly what would change (any
combination of the three, or none - no prompt at all if nothing would actually change). Declining
leaves it completely untouched; print configuration and APF still apply independently either way.

### Port resolution / `UseExistingPort`

Every row first checks whether a Standard TCP/IP port already targets its IP (registry-based,
`portlookup_windows.go`) and reuses it if so - this also doubles as the fix for ever creating a
genuine duplicate port for a host that already has one, e.g. on redeploy. If `UseExistingPort` is
checked and no such port exists, that's a fatal error for the row. Otherwise a new port is created,
named `<prefix><ip>` (or just `<ip>` with no prefix set), with a numeric suffix appended only if
that exact name is already in use by a *different* host.

## Building / testing

```
go build ./...
go vet ./...
go test ./...
```

`wails dev` / `wails build` build the actual application (see `wails.json`); the Go backend above has
no dependency on the frontend and is fully testable on its own.
