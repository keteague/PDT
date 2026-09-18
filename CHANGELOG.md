# Changelog

All notable changes to this project are documented here. This is a from-scratch Go/Wails rewrite of
`Create-Printers.ps1`; entries reference that original tool's own history where a decision or
limitation carries forward from it.

## 2026-09-17 (v0.9.29) - Resizable log panel, macOS flash-drive fixes (7-Zip tools, Spotlight slowdown), .pdt-infcache now travels with Sync, and automated cross-platform releases

(v0.9.28 was tagged but never published - its own first real CI run under the new
`release.yml` below failed on both platforms before either build finished: `go build`/`vet`/`test`
ran before `wails build`, but `main.go`'s `//go:embed all:frontend/dist` needs `frontend/dist` to
already exist, and it's gitignored - only `wails build`'s own pipeline produces it. Fixed by
reordering; the macOS job also hit a real bash 3.2 "unbound variable" bug referencing an array
built dynamically via `go list`, fixed by listing the exact package paths instead. This release is
that fix plus everything below, retagged.)

### Added
- A drag handle between the grid and the log panel (`#logResizeHandle`, `main.js`/`app.css`) - the
  log panel was a fixed 200px, with no way to see more of either the grid or the log at once.
  Height is clamped (80px log minimum, 150px grid minimum) and persisted to `localStorage` across
  restarts.
- `.github/workflows/release.yml` - pushing a `v<version>` tag now builds both platforms
  (`wails build` + Inno Setup on Windows, `wails build` + a zipped `.app` on macOS) and publishes
  a GitHub Release itself, with `PDT.exe` uploaded under that exact name (`CheckForUpdate`'s own
  lookup) alongside the installer and the macOS zip - replaces the fully manual "build locally,
  create the release by hand" process this README documented until now. `validate-version` refuses
  to run either build at all if the tag doesn't match the `VERSION` file. The macOS build is
  ad-hoc signed only - issue #9's own local-only "PDT Local Dev" signing workaround can't exist on
  a GitHub-hosted runner, so privileged Deploy will still hit its AMFI SIGKILL on whatever machine
  opens this build until a real Developer ID signs it instead.

### Fixed
- **macOS: Write to Flash Drive never included the Windows `tools/7zip` folder** (`7z.exe`/
  `7z.dll`/`License.txt`) a portable copy needs to extract Lexmark's self-extracting RAR package -
  `writePortablePDTTo` copied it from `sevenZipToolsDir()`, a Windows-only per-machine cache
  `app_darwin.go` always resolved to `""`. The embedded 7-Zip assets (`sevenzip_windows.go`) moved
  into a new cross-platform file (`sevenzipassets.go`) so a macOS build carries the same bundled
  bytes too - it never runs them, but Write to Flash Drive now writes them straight onto the flash
  drive on every platform, so a flash drive built on a Mac is just as usable on a locked-down
  Windows machine as one built on Windows itself.
- **macOS: Write to Flash Drive/Sync to a real USB drive was slower than it needed to be** -
  macOS indexes a newly-written external volume via Spotlight by default, and `mds_stores`
  actively crawling the very files being written competes for I/O against the copy itself.
  `flashdrive.DisableIndexing` (`mdutil -i off`, no sudo needed for a user-mounted volume) now
  runs before both Write to Flash Drive and Sync. Measured live against a real 15GB USB drive and
  a real ~7,000-file/719MB driver subset, two runs each: indexing enabled (214s, 212s) vs.
  disabled (195s, 179s) - a consistent, repeatable ~12-16% speedup, not noise.

### Changed
- **`.pdt-infcache` is no longer excluded from Write to Flash Drive, Sync, or Cloud Sync** - it
  used to be skipped entirely, the same as an archive's derived "extracted sibling" folder, but
  unlike that folder it's real, useful catalog metadata (the `.inf`-only extraction cache issue
  #10's catalog rework builds), not disposable clutter. Every *other* dotfile/dotfolder
  (`.DS_Store`, a stray `.git`, etc.) is now skipped too, generalized from the old `.DS_Store`-only
  check (`driver.IsIgnoredDotEntry`) - both `copytree.go`'s flash-drive/Sync copy and Cloud Sync's
  `internal/cloudsync/plan.go` (local walk and remote listing alike) enforce the same rule.

## 2026-09-16 (v0.9.27) - Windows: fixed a cross-platform test gap in the write-protected-media fallback test, first Windows build since v0.9.22

Pulled in v0.9.22 through v0.9.26 (macOS-side work: lazy zip extraction, real Lexmark support,
the write-protected-media extraction fallback, an installer-version-gate fallback, a Canon PPD
locale-bucket fix, and stale .dmg mount cleanup) - none of those releases had a Windows build
attached, so this is the first Windows installer/exe published since v0.9.21.

### Fixed
`TestEnsureArchiveExtracted_FallsBackWhenArchiveDirIsWriteProtected`
(`internal/driver/lazyextract_test.go`, added in v0.9.24) simulates a write-protected removable
drive via `os.Chmod(dir, 0o555)` - which enforces real POSIX write-protection on macOS/Linux, but
not on Windows: `os.Chmod` there only toggles the read-only file attribute, and doesn't block
`MkdirAll`/file creation inside a directory the way Unix permission bits do. The primary
extraction attempt inside the "protected" directory silently succeeded on Windows, so the
fallback the test exists to verify never actually triggered, failing the test - the same class of
platform gap the test already guarded against for root (which also bypasses Unix permission
bits), just missing the Windows case. The underlying `extractWithFallback` logic itself is
platform-agnostic and unaffected - a real write-protected flash drive genuinely fails the primary
write on Windows too; this was purely a test-simulation gap. Fixed by skipping this test on
Windows with an explanation, matching the existing root-skip's own style.

## 2026-09-16 (v0.9.26) - macOS: fixed issue #1 - stray .dmg mounts left behind by an interrupted catalog build

A full audit of mountDmg/LocatePkgWithChain/LocateLoosePPDs and every one of their 6 real call
sites found no bug in the cleanup logic itself - every path correctly composes and calls its own
detach func, including the nested-.dmg case and every error path. But a live check on this same
machine found 3 real, currently-mounted stray volumes (a Canon UFR II outer+nested .dmg, a
Toshiba .dmg.gz) with mounting process IDs that had long since exited - the leak is real, just not
where the issue's own checklist pointed.

**Root cause**: app.go's own `startup()` calls `loadCatalog` synchronously as part of Wails' own
startup lifecycle, mounting real packages along the way. If the whole process is terminated (a
crash, a force-quit, or an external SIGTERM arriving mid-build) before that call stack unwinds
normally, no deferred cleanup ever runs - Go's `defer` only fires on a normal return within the
same goroutine, not when the whole process is torn down out from under it.

### Added
- `driver.ReconcileStaleMounts` (`internal/driver/macmountreconcile.go`) - sweeps `hdiutil info`
  for any real, currently-mounted volume this codebase's own `mountDmg` would have created but
  never got a chance to unmount (an image-path either living under a `pdt-`-prefixed OS temp
  directory - Canon's own zip-extraction detour, Toshiba's own gzip-decompression detour - or
  directly under the real Drivers folder - every other manufacturer's own real `.dmg`, mounted
  straight from its real location), and detaches them, nested-first. Wired into `platformStartup`,
  before `loadCatalog` gets a chance to mount anything new - a stray `CANON_MAC` left over from a
  previous run would otherwise force a fresh mount of the same real volume to rename itself
  `CANON_MAC 1`, the exact symptom the issue itself first reported.
- Never touches anything that isn't demonstrably this codebase's own - confirmed live and via a
  dedicated safety test that a real, unrelated mount (something the user mounted themselves) is
  never matched.

Confirmed live end-to-end, twice: directly (recreating the real leak, then confirming
`ReconcileStaleMounts` cleans it up) and through the real, signed app itself (launching `PDT.app`
with the stray mounts present, confirming they're gone by the time startup completes).

## 2026-09-16 (v0.9.25) - macOS: fixed issue #6 - LocateLoosePPDs silently dropped every locale but one for Canon's own restructured PPD bucket

Found while live-verifying issue #4 back in an earlier session, low-urgency and left for whenever
`macmount.go` was next touched. `LocateLoosePPDs`'s nested-.dmg fallback (`findFirstByExt`) only
ever mounted and indexed the *first* nested `.dmg` it found. Confirmed live against Canon's own
real "PPD" bucket download (`PPDv5.50_mac.zip`): its own locale variants are two nested `.dmg`
files side by side (`PS_PPD/mac-ppd-v550-uken-16.dmg`, `PS_PPD/mac-ppd-v550-usen-16.dmg`), both
containing the same real model's own PPD under the same real filename
(`CNADV529X1.PPD.gz`, "Canon iR-ADV 529") - "whichever mounts first" silently dropped the other
locale's own entire PPD set, every time.

### Fixed
- `locateLoosePPDsFromRealPath` now mounts and collects PPDs from *every* nested `.dmg` found
  (`collectByExt`, not `findFirstByExt`'s single match), matching the older flat-folder-per-locale
  version of this same package's own already-correct "collect everything" behavior - both real
  `PPDv5.50_mac.zip` locale variants now come back (confirmed live:
  `CNADV529X1.PPD.gz` found twice, once per locale, matching the older `PPDv5.35_mac.zip`'s own
  already-correct count). One bad nested `.dmg` (fails to mount) no longer blocks collecting PPDs
  from the others - best-effort, same discipline every other multi-item walk in this codebase
  already holds itself to.

## 2026-09-16 (v0.9.24) - Windows: issue #10's own portable-mode gap fixed - extraction falls back off a write-protected flash drive

Issue #10 already shipped its own 3-increment rollout (Sync skipping extracted sprawl, catalog
scans no longer extracting eagerly, the Selective Rescan dialog) - but stayed open for one real
gap its own "Portable-mode interaction" section flagged and left unresolved: on-demand extraction
(`EnsureArchiveExtracted`) always writes as a sibling of the archive itself, wherever that archive
happens to sit. Running PDT portably from a write-protected flash drive - Ken's own real
field-deployment scenario, plugging into a client endpoint it's never touched before - made that
write fail outright, with no fallback at all, breaking Deploy entirely for that whole scenario.

### Added
- `EnsureArchiveExtracted` now falls back to a throwaway local-disk scratch directory whenever
  the normal sibling-of-the-archive extraction fails for any reason (`extractWithFallback`,
  `internal/driver/lazyextract.go`) - deliberately doesn't try to distinguish *why* the first
  attempt failed (a genuinely corrupt archive fails identically in both locations, at the cost of
  one harmless extra attempt) rather than pattern-matching per-tool (7z/msiexec/archive/zip) error
  text. Per Ken's own explicit call on the issue's own open design question: the fallback cache is
  throwaway, not a persistent one keyed by archive identity - `EnsureArchiveExtracted` now returns
  a `cleanup func()` (a no-op for the normal, persistent case) that `deploy_windows.go` calls once
  done with a deploy, so a repeat deploy of the same driver from the same write-protected drive
  re-extracts from scratch every time rather than leaving a footprint scattered across however
  many different flash drives/sessions this ever runs from.
- New tests, including one that genuinely reproduces the failure (removes write permission on a
  real temp directory, confirms extraction still succeeds via the fallback and that `cleanup()`
  actually removes it afterward) - `internal/driver/lazyextract_test.go`.

Verified via `go build`/`go vet`/the full test suite, both natively and cross-compiled
(`GOOS=windows`) - this machine can't run the real `.exe` to confirm live on real Windows/real
flash-drive hardware, so that's still the honest next step before fully closing the loop, the
same discipline this project already holds every Windows-side change to.

## 2026-09-16 (v0.9.23) - macOS: issue #12's installer-version-gate fallback, confirmed live end-to-end

Same-night follow-through on v0.9.22's own newly-filed issue #12: designed, built, and - after
three real live-deploy-driven bug fixes in a row - confirmed working end-to-end against Ken's
own real, previously-failing Ricoh package.

### Added
- **Issue #12 fixed**: a batched row whose package fails `installer`'s own version-check
  predicate (confirmed live: `This update requires macOS version 15.0 or earlier.`) now falls
  back to extracting that package's own real driver footprint - not just the PPD, but its
  supporting filter binaries/PDEs/icons too (confirmed necessary: Ricoh's own real filter lives
  in a *separate* sub-package from its PPD) - and placing it by hand, bypassing `installer`'s own
  version gate entirely. Guarded by Ken's own explicit threshold (only for a package sitting in a
  macOS 14+ folder) and a keyword match against the real failure text, so an unrelated installer
  failure still surfaces honestly. Applies to both the batched path (all six "plain full install"
  planners) and the older non-batched fallback.
- Genuinely lazy: the fallback's own extraction only runs *after* `installer` has actually
  failed, deferred into the same already-authenticated elevated call via a hidden self-re-exec
  subcommand (`PDT __macversiongatefallback`) - not built speculatively up front for every row
  that merely sits in a 14+ folder. A real live regression this same night (an earlier, eager
  version of this fix) added ~29 seconds to an 8-row batch's own planning phase, most of it spent
  extracting packages that never needed the fallback at all; this version measures in
  microseconds for a row that doesn't need it.
- A local dev code-signing helper, `build-mac.sh` - `wails build` plus issue #9's own confirmed
  local-only re-sign workaround, since a plain `wails build` re-signs ad-hoc every time and
  silently re-triggers issue #9's own AMFI SIGKILL crash on the very next Deploy.

### Fixed
- Two real bugs found only by testing this live, in order: the fallback's own file-discovery
  logic wrongly skipped a sub-package that declares no install-location at all (Apple's installer
  treats that the same as "/") - the exact shape Ricoh's own real legacy bundle uses, so the
  fallback never found anything to fall back to on the very package that motivated it. Then,
  once that was fixed, the fallback's own `chown -Rh root:admin /Library/Printers` swept the
  *entire* shared directory rather than just what it had extracted, and failed outright on
  Canon's own already-installed, code-signed `autoSetupTool.app` ("Operation not permitted",
  even running as root) - now scoped to chown only the exact files this fallback itself placed.
- Lexmark's real PPD spells its color/mono option `*ColorMode` (values `TrueM`/`FalseM`, via
  self-describing labels), not the standard `*ColorModel` PDT was matching - a real deploy always
  warned "declares no ColorModel option" despite a driver that does support both. Added the
  keyword; the existing label-matching machinery (already built for Sharp's similarly abbreviated
  values) handles the rest with no further change.

## 2026-09-16 (v0.9.22) - macOS: lazy zip extraction (issue #11), real Lexmark support, batched-OpenPrinting fallback, and a real Ricoh installer bug found live

Two real, live-driven arcs in one overnight session: finishing the mac-side mirror of #10
(driven by Ken deleting the same regenerated extracted folders by hand, twice), then - once
Ken added a real Lexmark package and asked for it supported - a real live deploy immediately
surfaced a genuine Ricoh installer bug that a same-night diagnostic fix made visible at all.

### Added
- **Issue #11**: `internal/driver/maczip.go`'s `ensureMacZipsExtracted` (eager, at every catalog
  build, permanent sibling folder - confirmed live to regenerate ~2.6G/36 folders on a real
  machine every single time) replaced with `resolveMacZipSource`: `scanMacPackages` now records
  a `.zip` as its own catalog entry directly, resolving it on demand into a throwaway temp
  directory only when something (cataloging, or a real Deploy) actually needs the real bytes.
  Also proactively deletes any leftover pre-fix sibling folder still on disk (`ExtractedSiblingDirs`).
  **Confirmed live**: Canon (475 models) and Konica Minolta (30 models, re-keyed from `.pkg` to
  `.zip` provenance) both re-indexed correctly against Ken's own real packages; zero extracted
  folders exist after the run, previously regenerated every time.
- **Real Lexmark support**: Ken's own real download (`Universal_Color_Print.pkg`) is a genuine
  Universal Print Driver - one PPD, no per-model list. Added `macFamilyPreference["Lexmark"]`,
  the same content-based PPD-extraction fallback Ricoh/Xerox/Toshiba/Konica Minolta already
  needed (no `.ppd` in the real filename), and `planLexmarkBatchRow` so it joins the shared
  1-auth-prompt batch instead of always paying its own separate prompt. **Confirmed live**: a
  real batched deploy installed Lexmark within the same one-second window as Canon/Kyocera/
  Sharp/Toshiba/Xerox/Konica Minolta - one shared auth prompt, not a separate one.
- Per-row stderr capture in `PrepareBatch` (`canonbatch_darwin.go`) - a failing batched row used
  to report nothing but `"batched install/queue-create failed (exit 1)"`, no detail on why. Each
  row's own subshell now redirects stderr to a per-row file, folded into the error message.
  **This is what actually revealed the real Ricoh bug below** - without it, that failure would
  still just read "(exit 1)".
- OpenPrinting fallback PPDs (the community-maintained generic bucket, never a real
  vendor-branded driver) now carry a trailing `" (OP)"` marker everywhere shown (`ppdMatchLabel`)
  - confirmed live as a real point of confusion during a Lexmark deploy that used one with
  nothing distinguishing it from a genuine driver. `DriverCandidates` now always offers matching
  OpenPrinting PPDs alongside whatever real driver/catalog match already resolved (not just when
  nothing else is available), so a technician can explicitly override an auto-resolved driver
  that doesn't actually cover their model. A row resolving *only* via OpenPrinting now also joins
  the shared batch (`planOpenPrintingBatchRow` - no install step, `lpadmin -P` straight against
  the loose PPD's own path) instead of paying its own separate elevated call.

### Found (not yet fixed - tracked as [issue #12](https://github.com/keteague/PDT/issues/12))
- A real batched Ricoh deploy failed: `installer: Error - This update requires macOS version
  15.0 or earlier.` - the real `installer` binary enforcing Ricoh's own `<installation-check>`
  version-gate predicate against a macOS release newer than Ricoh validated this download
  against. No supported flag bypasses this. Ken's own proposed fix: fall back to extracting the
  PPD directly from the payload (bypassing `installer` and its version gate entirely) only when
  the failure looks like this specific version check *and* the package's own `OSVersionFolder`
  is macOS 14+ - not yet implemented; open design questions (detecting the failure reliably from
  free-text stderr, and whether the PPD alone is enough or its own supporting filter files need
  extracting too, the same way Canon/Kyocera's own selective install already does) are on the
  issue itself.

## 2026-09-15 (v0.9.21) - Sync and Write to Flash Drive now ignore .DS_Store entirely

macOS creates a `.DS_Store` (Finder's own per-folder metadata) in nearly every folder it browses,
including a Drivers folder synced to/from a Windows machine - confirmed live sitting in a Cloud
Sync upload queue alongside real driver packages. Never real driver content, so nothing should
ever transfer it.

### Added
New `driver.DSStoreFileName` constant (`internal/driver/extractedsiblings.go`), alongside the
existing `PdtInfCacheDirName`, shared by every place that already had its own "what does Sync
skip" exclusion list:
- `copytree.go`'s `collectCopyJobs` - both flash-drive Sync directions and Write to Flash Drive
  are built on this.
- `internal/cloudsync/plan.go`'s `walkLocal` (the local side) and `listRemote` (the remote side -
  a `.DS_Store` already sitting in the bucket from before this exclusion existed now disappears
  from the plan entirely too, rather than showing as a spurious "Download" now that the local side
  never lists one).

New tests: `TestCopyTreeMerge_SkipsDSStore`, `TestListLocal_SkipsDSStore`,
`TestListRemote_SkipsDSStore` (the last against a fake S3 listing response, since the exclusion
happens while parsing that response, not in `diff()`).

## 2026-09-15 (v0.9.20) - Cloud Sync's tree mislabeled already-synced files as "Conflict"

Found live: after a Cloud Sync run got interrupted (see the incomplete-transfer report just above),
reopening Cloud Sync showed every file in a manufacturer folder as a red "⚠ Conflict" - alarming,
since a real conflict means local and remote genuinely differ and can't be resolved automatically.

### Fixed
Turned out none of it was real: the dialog's own hint text right above the tree ("Nothing to
sync - this computer and the cloud repository already match.") only ever shows when the real
conflict count is zero - which was true the whole time. `cloudSyncActionLabel` (`main.js`) was
written back when only `"upload"`/`"download"`/`"conflict"` were expected to reach it, and fell
through to the "Conflict" badge for anything else - but `GetCloudSyncPlan` actually returns every
item `BuildPlan` produces, `"synced"` (already matching, nothing to do) included, so an
already-synced file got the same alarming red badge as a genuine mismatch. Fixed with an explicit
`"synced"` branch (a neutral "✓ Synced" badge, distinct styling from both Conflict and
Upload/Download) plus a corrected tooltip on the non-actionable checkbox spacer, which was also
unconditionally claiming "sizes differ" even for an already-matching file.

## 2026-09-15 (v0.9.19) - Lexmark deploy failed with "SetupCopyOEMInf(...): The system cannot find the file specified"

Found live via a full 9-manufacturer deploy test on v0.9.18 - every other manufacturer succeeded;
Lexmark alone failed staging its driver.

### Fixed
Lexmark's real package is an outer self-extracting RAR whose own selective `.inf`-only extraction
(`extractInfsFromSfxArchive`, GitHub issue #10) also pulls out several inner `.msi` files by
design - needed to then find *their* own `.inf` entries in a second pass
(`ensureMsiInfsExtracted`). That means `ArchEntry.ArchivePath` for a Lexmark driver found this way
points at one of those inner `.msi` files, which already lives inside `.pdt-infcache` rather than
being a normal sibling file in the manufacturer folder.

`EnsureArchiveExtracted` (`internal/driver/lazyextract.go`) didn't account for that: its own
"already extracted, skip" check computes a nested archive's destination the exact same way
`infCacheDestDir` already does for it (a plain sibling, since it's already inside
`PdtInfCacheDirName`) - so it found the `.inf`-only cache folder the catalog scan had already
created there (containing just the cached `.inf`, none of the companion `.dll`/`.cat`/etc. files a
real deploy needs alongside it), assumed that meant full extraction was already done, and handed
that incomplete folder straight to `StageInf` - which then failed reading a companion file that
was never actually there.

Fixed by having `EnsureArchiveExtracted` recognize when `archivePath` lives inside
`.pdt-infcache`, fully extract the *outer* archive first (recursively - handles any nesting
depth), and re-resolve the nested archive to its own real position inside that now-complete
extraction before extracting it in turn. Confirmed live against the real Lexmark package: the
previously-failing `.inf` now lands alongside its full, real companion file set (`.cat`, several
`.gd_`/`.gp_`/`.in_`/`.tx_` compressed siblings, and per-language subfolders) rather than sitting
alone. New regression test:
`TestEnsureArchiveExtracted_NestedArchiveInsideInfCache` (a synthetic zip-in-zip fixture -
Lexmark's own real self-extracting-RAR-in-an-`.exe`/`.msi` shape needs 7z/msiexec to even
construct a fixture for, but a zip-in-zip exercises the identical code path without either).

## 2026-09-15 (v0.9.18) - Settings > General's Browse ("...") button could silently do nothing, and macOS gets the same install-dir hardening Windows got in v0.9.17

### Fixed
- **Settings > General's Browse button could silently do nothing at all**, for Configuration Files
  Base Path and Preinstall Base Path specifically (Drivers Base Path was unaffected). Root cause:
  Wails' own `runtime.OpenDirectoryDialog` refuses to even show the native folder picker when
  handed a `DefaultDirectory` that doesn't exist on disk yet - it returns an error instead
  (confirmed directly in `pkg/runtime/dialog.go`). Nothing auto-creates
  `Documents\Preinstall`/`~/Documents/Preinstall` the way Drivers/Configs get scaffolded, and the
  three Browse click handlers (`main.js`) had no `.catch()`, so that rejected promise vanished with
  no visible sign of why - looking exactly like the button did nothing. Fixed with a new
  `nearestExistingDir` (`app.go`) that walks up to the nearest ancestor that actually exists (e.g.
  falls back to `Documents` if `Documents\Preinstall` isn't there yet) before handing a starting
  directory to the dialog, so Browse always opens somewhere sensible instead of erroring out; all
  three click handlers also now log a visible error on any future failure instead of failing
  silently. New regression test: `TestNearestExistingDir`.
- **macOS gets the same known-install-dir hardening `settings_windows.go` got in v0.9.17** (below):
  a new `isKnownInstallDir` (`settings_darwin.go`) recognizes an installed `.app` bundle sitting
  directly under `/Applications` or `~/Applications` (structurally - any bundle name, not a
  hardcoded `PDT.app` literal), so a stray Drivers folder ending up next to the exe inside an
  installed copy's own bundle can no longer get it silently reclassified as portable, the same real
  gap already fixed on Windows. Lower real-world likelihood on macOS (nobody casually drops a
  folder inside a `.app` bundle via Finder) but the same class of bug, so it gets the same fix for
  parity. New regression test: `TestIsKnownInstallDir` (darwin-only - not run on this Windows
  dev machine, verified via `GOOS=darwin go build`/`go vet` only).

## 2026-09-15 (v0.9.17) - Cloud Sync Cancel could still freeze on "Canceling...", and an installed copy's Drivers folder could get silently misdetected as portable

Two real bugs found live, both Windows-only.

### Fixed
- **Cloud Sync's Cancel button could still freeze on "Canceling..." forever.** The
  cloudSyncCancelGracePeriod fix already in place (see the v0.9.16 entry below) only bounded the
  per-file transfer join - it didn't cover SyncCloud rebuilding its own plan via `BuildPlan` at the
  very start of every sync, before any per-file transfer (and its ctx-aware cancellation) even
  begins. `listRemote`'s own `ctx` parameter (`internal/cloudsync/plan.go`) was accepted but never
  actually read - minio-go v7.3.0's `Core.ListObjectsV2`, unlike every other `Core` method this
  package uses, takes no `context.Context` at all, so a bucket listing still paging when Cancel
  landed had no way to be interrupted. Fixed in two layers: `listRemote` now checks `ctx.Err()`
  between pages (bounds the wait to one more page's own round trip rather than however many pages
  remain), and `SyncCloud` (`cloudsync_app.go`) now races its own `BuildPlan` call against ctx with
  the same grace-period backstop the per-file join already used, for the case where even a single
  page request hangs. New regression test:
  `TestListRemote_CancelBetweenPagesReturnsPromptly` (a fake server that pages forever, confirming
  `listRemote` still returns promptly once canceled).
- **An installed copy's Drivers folder could get silently reclassified as portable.**
  `defaultDriversBasePath`/`defaultSaveFileBasePath` (`settings_windows.go`) treat a Drivers folder
  sitting next to the running exe as a portable/flash-drive copy - correct for an actual portable
  copy, but a real gap for an *installed* copy: if a Drivers folder ever ends up sitting next to
  `PDT.exe` under `%ProgramFiles%\PDT` or `%LocalAppData%\Programs\PDT` (leftover from early
  testing, copied there by hand, etc.), PDT silently started treating that install as portable and
  used that exe-relative folder instead of `%LocalAppData%\PDT\Drivers` - splitting one
  technician's driver library across two locations with no visible indication of which one PDT was
  actually reading from. Fixed with a new `isKnownInstallDir` check: a Drivers folder next to the
  exe only means "portable" when the exe isn't running from one of PDT's own two known installed
  locations (the exact two `installer/pdt.iss` ever places it under). Installed copies now always
  use `%LocalAppData%\PDT\Drivers` regardless of what else happens to be sitting next to the exe -
  deliberately not because `%ProgramFiles%\PDT` isn't writable (`PDT.exe`'s own manifest requires
  elevation for every launch regardless of install location, so it is by the time PDT is running),
  but so an uninstall/reinstall of the program itself never risks touching a multi-GB,
  technician-curated driver library that has nothing to do with the program binary. New regression
  test: `TestIsKnownInstallDir`.

## 2026-09-15 (v0.9.16) - Selective Rescan dialog replaces the one-click Refresh Drivers button on Windows (GitHub issue #10, increment 3 of 3)

Increments 1 and 2 (below) stopped Sync from transferring extracted driver sprawl and stopped catalog
scans from producing it in the first place, using a `.pdt-source` marker file per `.pdt-infcache` entry
for both lazy-extraction resolution and orphan pruning - no separate `catalog.<mfg>.json` file was
needed after all (Ken's own call, once the marker mechanism turned out to already cover staleness on
its own).

This increment closes out the issue's original ask: a selective Rescan dialog, replacing the toolbar's
old one-click Refresh Drivers (🔄) button on Windows. Clicking it now opens a tree (manufacturer rows,
expandable to each one's own driver packages) with checkboxes, a **Select All** button, and a
Windows-only **Remove INF** checkbox that best-effort clears the selected packages' own
`.pdt-infcache` entries before rescanning, forcing them to be re-extracted fresh from their archive
rather than reused. New Go: `driver.ListRescanTargets` (the dialog's own tree data source - reuses the
exact same archive-recognition rules `ensure*InfsExtracted` already applies) and
`driver.RemoveInfCacheForSelection` (the "Remove INF" checkbox's own removal, keyed by
`"Manufacturer/RelPath"` strings the dialog's own leaf checkboxes report directly, no separate
encode/decode step), plus two new bound methods (`App.ListRescanTargets`/`App.RescanDrivers`) in
`drivercatalog_windows.go`.

The rescan itself is always a full `App.RefreshDriverCatalog()` regardless of what's checked in the
tree, not a rebuild scoped to just the selection - now that `.inf`-only extraction replaced full-package
extraction (increment 2), a full rescan is cheap regardless (bounded by archive count, not
extracted-file count), so there's no performance reason for separate partial-rebuild machinery just to
mirror the dialog's own selective removal scope; the selection only ever controls what "Remove INF"
touches.

macOS keeps the old one-click Refresh Drivers behavior completely unchanged - it has no
`.pdt-infcache`/"Remove INF" concept to be selective about, since its own `catalog.<mfg>.json`
staleness handling already runs automatically on every refresh (see the v0.9.2 entry below). The
toolbar button is now platform-branched in `main.js` (`btnRefreshDrivers`'s own click handler) rather
than split into two separate buttons, since both are "rescan my drivers" from the technician's own
point of view - just with a different amount of interaction to get there.

## 2026-09-15 - Catalog scans no longer extract whole driver packages (GitHub issue #10, increment 2 of 3)

Increment 1 (below) stopped Sync from transferring the extracted sprawl; this increment stops
producing it in the first place. `DriverNamesFromInf` (`internal/driver/inf.go`) only ever reads a
single `.inf` file's own `[Version]`/`[Strings]`/`[Manufacturer]` text - nothing else in a real package
(the actual `.dll`/`.cat`/help files, hundreds to thousands of them) is ever read to build the catalog.
So `BuildCatalog` no longer extracts whole packages at scan time at all - it extracts just the `.inf`
file(s) into a small `.pdt-infcache` folder, and defers full extraction to Deploy-time, on demand, for
only the one specific package actually being installed.

### Added
- **`.inf`-only extraction for all four archive types** (`internal/driver/{zip,sfx,msi,kyoceraexe}.go`):
  zip via `archive/zip` directly (no external tool needed at all); self-extracting archives and
  Kyocera's own two-stage exe via 7z's own selective-extraction filter (`7z x pkg -o<dest> *.inf -r`,
  pulling just the matching entries without unpacking the rest); MSI has no selective-extract mode via
  `msiexec /a`, so it still runs the full administrative install, but only ever into a throwaway scratch
  directory that's discarded immediately after the resulting `.inf` is copied out - the real payload is
  never kept. All four write into `<Manufacturer>/<version>/.pdt-infcache/<ArchiveName>/...`, preserving
  each `.inf`'s own path within the archive (needed for the existing arch-token detection, which reads
  path segments relative to the manufacturer folder).
- **`EnsureArchiveExtracted`** (`internal/driver/lazyextract.go`): the deferred full extraction, reusing
  the *existing* per-format extraction functions verbatim - zero change to the actual extraction logic,
  just when it runs. Wired into `internal/printer/windows/deploy_windows.go` right before `StageInf`
  (`SetupCopyOEMInfW` needs the real files on disk next to the `.inf` - a hard Win32 constraint, not a
  PDT design choice). Same skip-if-already-extracted convention - a repeat deploy of the same driver
  reuses the folder instead of re-extracting.
- **Automatic staleness cleanup, without a separate catalog file**: each `.inf`-only cache entry gets a
  small `.pdt-source` marker recording exactly which archive produced it (`ArchEntry.ArchivePath`/
  `InfRelPath` resolve through `Resolve()` from this). Every scan prunes any top-level cache entry whose
  recorded archive no longer exists, *before* the `.inf`-discovery walk runs - confirmed live this is a
  real correctness requirement, not just disk hygiene: without pruning first, a removed/archived
  driver's stale cached `.inf` would keep being found and parsed, keeping it visible in the catalog
  indefinitely. Pruning itself is best-effort and never required to succeed for correctness on a
  write-protected flash drive (the field-deployment plan) - a failed cleanup just means the stale entry
  sits there until the next writable run, never a wrong catalog result.
- Considered building this as a real `catalog.<mfg>.json` file for consistency with the existing macOS
  `catalog.<mfg>.json` (`internal/driver/maccatalogdb.go`) - decided against it once the `.pdt-source`
  marker mechanism above turned out to already fully satisfy the staleness requirement on its own,
  without a second persisted format to keep in sync. `MacCatalogFileName` was still renamed to the
  platform-neutral `CatalogFileName` (pure string logic, nothing mac-specific) in case Windows ever does
  grow a real catalog file for a different reason later.

### Fixed
- **A real bug found via live testing against Lexmark's actual package** (an outer self-extracting RAR
  wrapping an inner `.msi` that itself contains the real `.inf`): the first cut of this rework skipped
  `.pdt-infcache` entirely while searching for source archives, confusing it with Sync's own unrelated
  "never transfer this" exclusion - which silently broke the cascade, since the `.msi` only exists
  *inside* the cache once the sfx pass reveals it there. Fixed by no longer skipping the cache directory
  while searching for archives (only Sync does that, for an unrelated reason), and by extending the sfx
  extraction filter to also pull out any `.msi` it finds, not just `.inf`.
- A related destination-path bug caught by the same fix: a nested archive already living inside
  `.pdt-infcache` was computing a doubled `.pdt-infcache/.pdt-infcache/...` destination instead of
  extracting as a plain sibling where it already sat.

### Verified live
Full real-world validation against this dev machine's actual Drivers folder (not testdata): all 9
manufacturers and 555 driver names found correctly (matching the pre-rework catalog), including
Lexmark's real sfx→msi cascade (6 `.inf` files) and Kyocera's real two-stage package (4 `.inf` files,
~1MB total vs. ~12,700 files/GBs for a full extraction). End-to-end resolve→lazy-extract→deploy path
confirmed against a real Canon package: `Resolve()` correctly returns `ArchivePath`/`InfRelPath`,
`EnsureArchiveExtracted` produces a real `.inf` with its real companion files alongside it, and a
repeat call reuses the extraction instead of redoing it. Staleness confirmed end-to-end too: removing a
test archive and rebuilding the catalog correctly drops that driver and prunes its orphaned cache entry.

**Correction to increment 1's own "bonus" note**: measured (not just reasoned about) whether
`app_windows.go`'s `loadCatalog` could now drop its removable-media special-case
(`BuildCatalogNoExtract`), since `.inf`-only extraction is so much cheaper than a full one - it can't,
yet. The four new extraction functions still each `filepath.WalkDir` a whole manufacturer folder
looking for source archives, and a real Drivers folder still has all its *legacy* full-extraction
sprawl sitting alongside the new `.pdt-infcache` (nothing deletes that automatically) - measured at
~4.8 seconds just for those four walks on this nVME dev machine's real Drivers folder, which would be
dramatically worse over real USB 2.0. `BuildCatalogNoExtract`'s removable-media path is unchanged.

## 2026-09-15 - Sync no longer transfers extracted driver sprawl (GitHub issue #10, increment 1 of 3)

Ken's own live measurement of a real Drivers folder: **5.4GB/22,570 files** total, of which the
original compressed archives are only **~1.5GB/~25 files** - the rest is extraction sprawl
`BuildCatalog` (`internal/driver/catalog.go`) produces eagerly and never deletes. Both flash-drive Sync
(`copytree.go`) and Cloud Sync (`internal/cloudsync`) were transferring that entire sprawl alongside
the archives, with no concept of "this folder is a derived, re-creatable artifact of that archive
sitting next to it." This is increment 1 of 3 toward closing the issue (see it for the full design) -
the low-risk Sync-side fix, shippable against today's existing eager-extraction reality while the
bigger catalog rework (stop extracting whole packages at all, cache just the `.inf` metadata, extract
fully only at Deploy-time - mirroring the existing macOS `catalog.<mfg>.json` pattern for consistency)
is still being built.

### Changed
- **New `driver.ExtractedSiblingDirs`** (`internal/driver/extractedsiblings.go`): given one directory's
  own children, identifies which subfolders are the deterministic extraction output of an archive also
  in that directory - `Foo.zip`/`.msi`/a real self-extracting `.exe` -> `Foo/`, or Kyocera's own
  `KXDriver_<version>.exe` -> whichever sibling folder name *contains* that version token (mirroring
  `kyoceraVersionAlreadyExtracted`'s own existing substring match) - computed without extracting
  anything.
- **Both Sync paths skip these folders entirely**, not just filter them from the result:
  `copytree.go`'s `collectCopyJobs` (flash-drive Sync/Write to Flash Drive) and a rewritten
  `cloudsync.listLocal` (`internal/cloudsync/plan.go`, now a manual recursive walker instead of a flat
  `filepath.WalkDir`, so it can see a whole directory's sibling list at once the same way
  `collectCopyJobs` already does) both skip any folder `ExtractedSiblingDirs` flags, plus a new
  `.pdt-infcache` folder name reserved for the upcoming catalog rework's own `.inf`-only metadata
  cache. Skipping the directory outright (not walking into it and filtering after) also avoids paying
  the walk cost for whatever's inside it.
- **Measured on this dev machine's real (mixed Windows+macOS) Drivers folder**: 29,620 files/8.25GB ->
  7,081 files/4.65GB for what Sync would transfer - real, but short of the issue's own ~25-file ideal,
  which needs increment 2 (this machine's folder also has macOS content and `Archive/` folders,
  neither touched by this change, which is correct - not part of this problem).

## 2026-09-14 (v0.9.15) - Cloud Sync: rolling progress dialog, 95%-early-start pipelining, and a live Cancel-hang fix

Ken tested v0.9.14's Cloud Sync live and asked for the progress dialog to be far less basic (a
rolling "current file" spotlight with its own path/ETA, a queue of what's left, a total-batch
bar/rate/ETA, +60px wider), for the next queued file to start once the current one reaches 95%
rather than waiting for it to fully finish, and for Concurrent Transfers to be a real Settings
field (default 3) instead of a fixed constant. Separately, a real live bug: canceling an
in-progress upload left the Cancel button frozen on "Canceling..." forever.

### Added
- `internal/cloudsync`'s `Upload`/`Download` are unchanged, but `SyncCloud` (`cloudsync_app.go`)
  was rebuilt around a semaphore whose slot releases the moment a file reaches 95% done (not
  strictly 100%) - the next queued file starts while the outgoing one finishes its own last few
  percent (often mostly finalization overhead: a multipart `CompleteMultipartUpload` round trip,
  a final rename) instead of a lane sitting idle waiting that out. Actual simultaneous transfers
  can transiently run a little over the configured count right at a handoff.
- Settings > Cloud Sync gained a **Concurrent Transfers** field (default 3, clamped to at least 1
  on save) - the semaphore's own size above, previously a hardcoded `cloudSyncWorkers = 4`
  constant.
- The progress dialog was fully redesigned: a rolling single "spotlight" file (the
  longest-running transfer still in progress) with its own full path, a big bar, and a per-file
  ETA (`newCloudSyncProgressFunc` now carries its own `etaEstimator`, the same per-step machinery
  flash-drive Sync's own progress dialog already uses); everything else concurrently transferring
  or still queued shown below it (a concurrent file gets its own small inline bar); a whole-batch
  total progress bar, transfer-rate meter, and ETA at the bottom, fed by a new
  `cloudsync-total-progress` event (`CloudSyncTotalProgress`) alongside the existing per-file
  `cloudsync-progress` one. `etaEstimator` gained a `rate()` method (`flashdrive.go`) for the
  meter. Widened +60px (460px -> 520px) - Ken's own explicit ask, since this dialog now carries
  real content (a full path, a rate meter, a queue) the generic confirm-style modals sharing the
  default width don't.

### Fixed
- A real live bug: canceling an in-progress Cloud Sync upload could leave the Cancel button stuck
  on "Canceling..." indefinitely. Two new regression tests
  (`internal/cloudsync/cancel_hang_test.go`, `cancel_hang_multipart_test.go`, both against a real
  `httptest` server standing in for R2) confirm `Upload`'s own context-cancellation handling
  returns in well under a second for both the simple and multipart code paths - the hang couldn't
  be reproduced in isolation, meaning it's most likely a real, possibly-stalled R2 connection not
  unblocking as promptly as `context` cancellation is normally guaranteed to. Rather than leave
  that open-ended, `SyncCloud` now bounds how long it waits after Cancel for every in-flight
  transfer to unwind (`cloudSyncCancelGracePeriod`, 10s) before giving up and returning anyway -
  whatever's still stuck keeps running in the background rather than being reflected in the
  result, but the dialog is now guaranteed to close either way.
- A stray `PDT` binary (a leftover local `go build .` artifact, never meant to be committed) was
  removed from the repo root; `/PDT` added to `.gitignore` so a bare `go build .` in the root
  can't reintroduce it.

## 2026-09-14 (v0.9.14) - Flash-drive Cancel/bidirectional Sync, and a full Cloudflare R2 Cloud Sync feature

A live crash report (PDT's window disappearing right as the elevation auth prompt appeared)
turned into a real, confirmed finding: ad-hoc code signing gets SIGKILLed by macOS AMFI during
privileged escalation (`Error Domain=AppleMobileFileIntegrityError Code=-423`), reproduced via a
real `zsh: killed` on direct execution and worked around locally with a self-signed cert -
tracked as [GitHub issue #9](https://github.com/keteague/PDT/issues/9) rather than fixed here,
since the real fix needs a paid Apple Developer ID.

Separately, Ken asked for three things: a Cancel button on flash-drive Sync (stop immediately,
delete whatever file was mid-copy), true bidirectional flash-drive Sync (Flash Drive -> This
computer, not just the other way), and - after learning PDT would have multiple technicians
sharing driver downloads - a shared Cloudflare R2 bucket every technician can sync their own
Drivers folder against, so a driver one technician downloads becomes available to everyone else
without a flash drive changing hands.

### Added
- Cancel button on the flash-drive copy-progress dialog (`copyTreeMerge`/`copyFile`,
  `copytree.go`) - stops as close to immediately as possible: no new file starts, and the one
  file each of the 4 worker goroutines is mid-copying is aborted and its partial destination
  content deleted, never left as a truncated stand-in for the real file.
- Bidirectional flash-drive Sync: the Sync modal now has a direction toggle ("This computer ->
  Flash Drive" / "Flash Drive -> This computer") backed by a new `SyncDriversFromFlashDrive`
  App method - previously Sync only ever copied laptop -> flash drive.
- Local copies (Sync/Write to Flash Drive) now preserve each file's original modification time
  instead of stamping today's date on every copy (`copyFile`'s own `os.Chtimes` after the copy
  completes).
- **Cloud Sync**, a new toolbar button (vertical bidirectional-arrows icon) and full feature for
  keeping this computer's Drivers folder in sync with a shared Cloudflare R2 bucket:
  - New `internal/cloudsync` package (`github.com/minio/minio-go/v7` against R2's S3-compatible
    API): `BuildPlan` diffs the local Drivers folder against the bucket by size (upload/download/
    already-synced/conflict - a size mismatch is never auto-resolved in either direction, only
    ever surfaced); `Upload`/`Download` are genuinely resumable - files at or above 32MiB use
    real S3 multipart upload with per-part resume (an interrupted upload, a lost connection, or
    PDT simply being closed mid-transfer all continue from the parts already on the server, not
    from byte zero), and downloads resume via HTTP Range requests against a `.pdt-partial`
    sidecar file.
  - `PauseGate`: Pause blocks between reads without losing any already-transferred bytes (Resume
    continues mid-file); Cancel is immediate and destructive on purpose - deletes a download's
    partial file and aborts an upload's incomplete multipart upload on the bucket (R2, like S3,
    bills for abandoned multipart storage otherwise).
  - Sync is additive-only, matching flash-drive Sync's own philosophy - a file missing on one
    side only ever triggers a copy, never a deletion, so no technician's local mistake (or a
    bug) can cascade into removing the shared bucket's content for everyone else.
  - Each object's original mtime round-trips through a custom `x-amz-meta-mtime` object
    metadata field (S3-compatible storage otherwise always stamps `LastModified` as upload time)
    - restored locally via `os.Chtimes` once a download completes.
  - Settings gained a Cloud Sync tab (Endpoint/Bucket/Folder/Access Key ID, pre-filled with
    Ken's own bucket) - the Secret Access Key itself is stored in the OS keychain
    (`github.com/zalando/go-keyring`) via a new `CloudSyncSecretKey` write-only field on
    `Settings`, never written to `settings.json` and never echoed back to the UI once saved.
  - The Cloud Sync modal is a hierarchical tree view (built client-side from the flat plan's own
    relative paths) with per-file and tri-state per-folder checkboxes; the selection persists
    per-technician across sessions (`cloudsyncstate.go`'s own `Deselected` set, defaulting every
    new/unknown file to selected - "everyone benefits from new drivers"). A file the tree hasn't
    shown this technician before is highlighted (amber, plus a small dot) until the next time
    the tree is opened, then the highlight clears for good (`cloudSyncState.Seen`).

### Fixed
- `applySalesChainGate()`'s own exemption list (`main.js`) didn't include the two new Cloud Sync
  modal backdrops, so every button and checkbox inside them - Close, Sync Selected, Pause,
  Cancel, and the tree's own checkboxes - stayed disabled whenever no Save ID was entered yet,
  caught live from a screenshot before this shipped.

## 2026-09-13 (v0.9.13) - macOS: Konica Minolta's "(S)" duplicate models dropped entirely (60 -> 30)

Ken asked what the real "(S)" PPD variant meant - answering it meant checking the real
package's own `Resources/en.lproj/Localizable.strings`, which spells it out directly: "TITLE" =
"Print (2-Sided) Driver Default", "TITLE_S" = "Print (1-Sided) Driver Default". Confirmed
against the real PPD content too - for the exact same physical model, the plain PPD's own
`*DefaultKMDuplex` is "Double" and the "(S)" PPD's is "Single", nothing else differs (same 30
real model numbers in both sub-packages, one-to-one, same underlying PDE/framework bundles).
Ken's own conclusion: since PDT always sets its own explicit Duplex default on every queue it
creates anyway, the "(S)" copy offers no real capability PDT doesn't already control - drop it
from the Model dropdown entirely rather than surface it as a second, misleading "model."

### Changed
- `konicaMinoltaIsSimplexDefaultVariant`/`konicaMinoltaCleanNickNames` (`mackonicaminolta.go`)
  now drop every "(S)" PPD outright during indexing, rather than keeping it as a separately
  selectable model (v0.9.12's own original design, made before the real meaning of "(S)" was
  known). Halves Konica Minolta's own real model count from 60 down to the 30 physical models
  that actually exist - confirmed live via `pdtdebugmac models`.

## 2026-09-13 (v0.9.12) - macOS: real Konica Minolta driver support (60 models) - and a real PPD-parsing bug found along the way

Ken: "Let's do Konica Minolta" - the last unexplored manufacturer. Real files needed real fixes
at every layer before any catalog work could even begin.

### Fixed
- `ensureMacZipsExtracted` (`maczip.go`) only did a single extraction pass - a real Konica
  Minolta download wraps two region subfolders, each holding its own *inner* zip wrapping the
  real `.pkg` (two levels of zip nesting, not the one level Canon's own shape needed). A single
  pass never discovers a zip that's only created *by* that same pass's own extraction, silently
  leaving the real `.pkg` permanently unextracted with no error at all. Now repeats the whole
  walk (capped at 5 passes) until a pass finds nothing new to extract.
- `flattenRedundantWrapperDir` (`flatten.go`) required literally the only top-level entry to be
  the redundant wrapper folder - a real Konica Minolta zip's own top level has both the real
  wrapper folder *and* a stray Finder-authored `.DS_Store`, defeating the check entirely. Now
  ignores a stray `.DS_Store` when checking for the single-folder shape. Fixing this uncovered a
  second bug in the same function: after moving the wrapper folder out, the leftover
  `.DS_Store` made the cleanup step's plain `os.Remove` (which requires an empty directory) fail
  silently, undoing the flatten - switched to `os.RemoveAll`.
- `ppdOpenUIRe` (`printdefaults_darwin.go`) required exactly one space between `*OpenUI` and the
  keyword - a real, previously-undiscovered bug: every single `*OpenUI` line in every real
  Konica Minolta PPD inspected uses a literal double space (confirmed via a raw hex dump), so
  this matched **zero** options in any real Konica Minolta PPD at all. Duplex/ColorModel would
  never have been set on a real deploy, with no warning either (the whole parsed-options list
  just came back empty). Now matches one-or-more spaces.
- `findOption` now recognizes Konica Minolta's own real Duplex-equivalent keyword, `KMDuplex`
  (choices Single/Double/Booklet, no NoTumble/Tumble binding-edge split at all) - its own real
  ColorModel option is the one manufacturer inspected so far that already uses the plain
  CUPS-standard spelling, needing no addition. `decidePrintDefaults`'s own one-sided/two-sided
  want-lists grew "single"/"double" - neither existing want-list vocabulary
  ("none"/"simplex"/"notumble"/"tumble"/"duplex") matched these choices at all, so a one-sided
  request would have silently matched nothing and left the PPD's own hardcoded 2-sided default
  in place.

### Added
- `macFamilyPreference["Konica Minolta"] = {".pkg"}` - unlike every other single-driver-line
  manufacturer here, no real filename anywhere in the chain (outer zip or the real `.pkg`
  discovered after extraction) ever contains "Konica" or "Minolta", and no single model-number
  substring survives across all 3 real download generations either. `.pkg` is used instead -
  every `MacPackage` entry already ends in `.pkg` or `.dmg` by construction, so it reliably
  matches without depending on an accidental substring that could break on the next download.
- Every real download splits into two paper-region variants, `WW_A4`/`A4` and
  `WW_Letter`/`Letter` (confirmed genuinely different files - different sizes, different
  checksums, not duplicates) - asked Ken which PDT should use; his call was Letter only, matching
  every other US-region default already established (HP's own "raw", Xerox's own "lp").
  `isKonicaMinoltaA4RegionDir` (`mackonicaminolta.go`) skips the A4 folder entirely during the
  catalog scan - the two region copies share the exact same real filename, so this can't be done
  by `classifyMacFamily`'s own basename-only convention at all.
- `konicaMinoltaCleanNickNames` strips the generic, non-distinguishing `" PS"` suffix every real
  PPD's own `*NickName` carries (Konica Minolta ships no non-PostScript language variant at all)
  while deliberately preserving a real `"(S)"` qualifier some models also carry - it names a
  real, separately-installable driver variant (the package's own second, non-default installer
  choice, covering a genuinely different, non-overlapping set of model suffixes), not a naming
  difference; collapsing it away would silently merge two real driver variants under one name.
  `macPPDEntryExpander`'s own dispatcher (previously Toshiba-only) moved to `macppd.go` and
  extended for this.
- `macSubPackagePPDFallback` extended to cover Konica Minolta - every real PPD named
  `KONICAMINOLTA<model>.gz`, no `.ppd` anywhere, the same real gotcha Ricoh/Xerox/Toshiba
  already had.
- `planKonicaMinoltaBatchRow` folds its own full `installer -pkg` run into the same shared
  1-auth-prompt batching - timing not yet live-confirmed (no real Konica Minolta deploy has run),
  flagged honestly since its own real package (59214 KB installed) is closer in scale to Xerox's
  than Ricoh/Sharp/Toshiba's much smaller ones.
- No Japan-market-only convention found across the 60 real PPDs inspected.

## 2026-09-13 (v0.9.11) - macOS: Toshiba's Driver field now shows exactly what macOS itself shows, no model name at all

Ken: v0.9.10's own fix still composed the Driver field as "<model> (<real driver>)" - e.g.
"TOSHIBA e-STUDIO2525AC (ColorMFP-S2)". He asked for it to stop mimicking the model at all and
just show the driver exactly as it appears in macOS's own Printers & Scanners > Printer Details
for that queue - i.e. the real PPD's own `*NickName` alone, "TOSHIBA ColorMFP-S2", with nothing
else appended (the model is already shown in its own separate Model field/column).

### Changed
- `macVariantLabel` (`macmodel.go`) replaces v0.9.10's `labelSuffix` - instead of only computing
  the parenthetical half of `"<model> (<suffix>)"`, it now builds a variant's own whole Label.
  For Toshiba specifically, that whole Label is just `"TOSHIBA " + toshibaDriverHintFromFilename(filename)`
  (`mactoshiba.go`, unchanged from v0.9.10) - the model name never appears in it at all. Every
  other manufacturer keeps the existing `"<model> (<family>)"` shape unchanged. Confirmed live
  that this reconstructs the real PPD's own `*NickName` byte-for-byte for all 8 real Toshiba
  files ("TOSHIBA_ColorMFP_S2.gz" -> "TOSHIBA ColorMFP-S2", "TOSHIBA_ColorMFP.gz" -> "TOSHIBA
  ColorMFP", etc.) - exactly what macOS's own Printer Details already shows for that queue.
  `decorateMultiVersionLabels`'s own multi-version-coexistence path takes an optional
  `versionTag` parameter now, appended in parens after the real driver name for Toshiba
  ("TOSHIBA ColorMFP-S2 (2026-01-15)") rather than after the model name - still dormant (no
  real Toshiba multi-version data exists yet), but consistent with the new shape.
  `MacVariantForDeploy`'s own deploy-time matching is unaffected - it matches purely by exact
  `Label` string equality, never assuming any particular shape.

**Confirmed live**: Ken deployed 3 real Toshiba models ("zKid Rock"/"zSalt Pepper"/"zSee Fit" -
one row each for the base ColorMFP, -X7, and -CN PDL variants) against the rebuilt app - each
resolved to and installed from its own correct real PPD file, exactly 1 elevated prompt for all
3 rows (the shared install completed in ~22s under that one prompt).

## 2026-09-13 (v0.9.10) - macOS: Toshiba's Driver field now shows the real PDL-variant name, not a repeat of the model

Ken: selecting "TOSHIBA e-STUDIO2525AC" (v0.9.9's own real model numbers) populated the Driver
field with "TOSHIBA e-STUDIO2525AC (Driver)" - reading as if the driver name just repeats the
model, when the real underlying file is "TOSHIBA ColorMFP-S2". Not a deploy bug (the correct
file was always installed) - just a real, confusing loss of genuinely useful information once a
model's own friendly name IS the real e-STUDIO number.

### Fixed
- `labelSuffix` (`macmodel.go`, new) replaces the bare `languageDisplayName(family)` call every
  variant's own Label suffix used - for every manufacturer except Toshiba this is unchanged
  (Canon's real "UFR II"/"PostScript"/"Generic PPD", or the generic "Driver" filler a
  single-token family like Kyocera/Xerox has always shown). For Toshiba specifically, it derives
  the real underlying PDL-variant name from the variant's own `Filename` instead
  (`toshibaDriverHintFromFilename`, `mactoshiba.go`: "TOSHIBA_ColorMFP_S2.gz" -> "ColorMFP-S2") -
  so the Driver field now reads "TOSHIBA e-STUDIO2525AC (ColorMFP-S2)", the real answer.
  Computed from `Filename` (already available and already persisted in `catalog.toshiba.json`
  everywhere a Label gets built) rather than threading a new field through
  `ppdEntry`/`MacPPDVariant`/`MacCatalogVariant` - no schema change needed, and the fix applies
  identically whether a variant comes from a fresh index build (`indexFamilyPackage`) or a
  cached catalog reload (`toMacPPDVariant`), plus the multi-version-coexistence label path
  (`decorateMultiVersionLabels`), which would otherwise have silently reverted to the generic
  filler the moment two Toshiba package versions ever sit side by side in the same OS folder.

## 2026-09-13 (v0.9.9) - macOS: real Toshiba model numbers (136 models), not just 4 generic PDL variants

Ken added `TOSHIBA_MonoMFP.dmg.gz` and asked whether the Color PPDs could be used on a B&W MFD -
answering that question meant inspecting each PPD's own `*Product` lines for the first time,
which turned up real per-model data v0.9.8 never looked for. Ken then asked to rebuild Toshiba's
catalog support around it.

### Changed
- v0.9.8 surfaced Toshiba's 4 (now 8, with Mono) generic PDL/controller-generation PPD names
  directly as the selectable "models," since inspecting only `*NickName` (generic per file, e.g.
  "TOSHIBA ColorMFP-X7") found no real model number anywhere. Each of those 8 real files
  actually declares 9 to 29 `*Product` lines (128 total) naming every specific e-STUDIO model it
  covers (e.g. `*Product: "(TOSHIBA e-STUDIO6570C)"`) - confirmed live that Color models always
  end "C"/"AC"/"CS" and Mono models never do, matching the color/mono question that started this.
  `toshibaExpandProductEntries`/`toshibaCanonicalModelName` (`mactoshiba.go`, new file) expand
  each generic file-level entry into one entry per real model instead, so the Model dropdown now
  shows 136 real e-STUDIO numbers - the same convention every other manufacturer's own dropdown
  already uses - instead of 8 generic names a technician needed Toshiba's own documentation to
  map to their hardware.
  - Real dedup needed: the same physical model can appear as more than one differently-spelled
    raw `*Product` line (e.g. "TOSHIBA e-STUDIO5008LP_Loops-LP50" vs "...5008LP Loops-LP50" vs
    "e-STUDIO5008LP_Loops-LP50" - underscore/space and an inconsistent "TOSHIBA " prefix) -
    `toshibaCanonicalModelName` normalizes and dedupes these down to one real model entry.
- `ReadPPDProducts` (`macppd.go`, new) reads every `*Product` line a PPD declares - the same
  transparently-gzip-decompressing read `ReadPPDNickName` already does, factored into a shared
  `readPPDTextBytes` helper both now call.
- `packagePPDEntriesFilteredFallback`/`indexFamilyPackage` gained a third optional hook
  (`expand func([]ppdEntry) []ppdEntry`) for this - and a real, confirmed-live bug along the
  way: the first version ran this hook in the *caller* (`indexFamilyPackage`), after
  `packagePPDEntriesFilteredFallback` had already returned - by which point its own
  `defer os.RemoveAll(tmpDir)` had already deleted every extracted PPD file, so
  `ReadPPDProducts` always failed and silently fell back to the unexpanded generic entry every
  time (Toshiba's own model count came back as 8, not ~136, until this was caught). Fixed by
  running the hook inside `packagePPDEntriesFilteredFallback` itself, before its own cleanup.
  `PackagePPDNickNames` (the guess-based fallback's own pre-install model check) now applies the
  same expansion too, for consistency.

## 2026-09-13 (v0.9.8) - macOS: real Toshiba driver support (4 generic PDL variants) + a genuine mounting gap fixed

Ken: "Let's work on Toshiba" - the single real download placed
("TOSHIBA_ColorMFP.dmg.gz") turned out to be a plain gzip-compressed UDIF image, a shape none
of Canon/Kyocera/Ricoh/Sharp/Xerox's own real downloads have - `hdiutil attach` doesn't
auto-detect a bare gzip wrapper on its own ("image not recognized"), and the catalog scanner's
own extension check (`filepath.Ext`) only ever saw the trailing ".gz", silently never
cataloging the file as a package at all. Both needed real fixes before Toshiba could work.

### Fixed
- `isDmgLikePath`/`mountDmg` (`macmount.go`) now transparently gunzip-decompress a ".dmg.gz"
  path to a temp file before calling `hdiutil attach` - the decompressed copy's own cleanup is
  folded into the mount's `detach` func, since the mounted volume needs it to keep existing for
  as long as it stays mounted. `LocatePkgWithChain`/`LocateLoosePPDs`'s own gates switched from
  a bare `filepath.Ext(path) == ".dmg"` check to `isDmgLikePath`.
- `scanMacPackages` (`maccatalog.go`) now recognizes a ".dmg.gz" suffix too, not just the
  single-extension `macPackageExts` map lookup - a real package would otherwise be silently
  invisible to the whole catalog, never even reaching a "not a package" log line.
- `PackageLabel`'s own filename fallback only stripped the trailing ".gz" off a compound
  ".dmg.gz" name via a single `filepath.Ext`-based trim, leaving ".dmg" in the displayed label
  ("TOSHIBA_ColorMFP.dmg" instead of "TOSHIBA_ColorMFP"). Fixed with a second, narrowly-scoped
  strip specific to the ".dmg.gz" shape - deliberately not a second blind Ext-based strip, which
  would wrongly mangle a real filename with a legitimate dot in its own version number (e.g.
  Xerox's own "XeroxDrivers_5.19.3_2562.dmg").

### Added
- `macFamilyPreference["Toshiba"] = {"Toshiba"}` - the manufacturer's own name is always in the
  real filename, so a single token unlocks the catalog-driven model index the same trivial way
  Kyocera's/Xerox's own single tokens do.
- Genuinely different real shape from every other manufacturer here: Toshiba's own sub-package
  (identifier `com.toshiba.pde.x7.colormfp`, install-location `/`) holds only 4 real PPDs total
  - "TOSHIBA ColorMFP", "-X7", "-S2", "-CN" - generic PDL/controller-generation variants
  covering Toshiba's whole e-STUDIO Color MFP line, not one PPD per specific model number the
  way every other manufacturer here works (see v0.9.9 above - this got real per-model data soon
  after). Every real PPD named `TOSHIBA_ColorMFP<suffix>.gz`, no `.ppd` anywhere - the same real
  gotcha Ricoh/Xerox already had; `macSubPackagePPDFallback` extended to cover Toshiba with the
  same shared `pathFragmentPPDExtractionFallback`.
- `findOption` (`printdefaults_darwin.go`) now recognizes Toshiba's own real ColorModel-
  equivalent keyword, `ColorType` (`*OpenUI *ColorType/Color Type: PickOne`, choices
  Auto/Color/Mono/Black&Red) - "Mono" is already self-describing, so no label-matching gap.
- `planToshibaBatchRow` (`canonbatch_darwin.go`) folds Toshiba's own full `installer -pkg` run
  into the same shared 1-auth-prompt batching - by far the smallest real package of any
  manufacturer here (6649 KB installed), so unlikely to be a speed concern, though (like Xerox)
  no real Toshiba deploy has actually timed it live yet.
- Only Color MFP models are covered by the one real download placed so far - no separate
  monochrome-line driver exists in the Drivers folder yet.

**Confirmed live**: Ken ran a real 4-row Toshiba deploy ("zFoo Bar"/"zThe Wire"/"zGreek Olive"/
"zLawyer Present" - one row per real PDL variant: ColorMFP, -CN, -S2, -X7) against the rebuilt
app - exactly 1 elevated prompt for all 4 rows (the shared install completed in ~32s under that
one prompt), no ColorModel warning on any row.

## 2026-09-13 (v0.9.7) - macOS: real Xerox driver support (178 models)

Ken: "Let's work on Xerox" - the Drivers/macOS/Xerox folder was completely empty at first
(confirmed: `ensureMacDriversScaffold` already correctly creates the bare manufacturer folder
for every entry in `driver.Manufacturers`, but deliberately never invents an OS-version
subfolder itself - there's no one macOS version safe to hardcode). Created the same 11
OS-version subfolders Canon already has (10.12-Sierra through 27-GoldenGate) for both Xerox
and Toshiba, each seeded with its own Archive/README.txt, so Ken could drop real downloads in.
Real Xerox files landed across 8 of them.

### Added
- `macFamilyPreference["Xerox"] = {"Xerox"}` (`macfamily.go`) - confirmed live against real
  files across all 8 populated OS-version folders that Xerox ships exactly one real driver
  line, periodically superseded ("XeroxDrivers_5.6.0_2187.dmg" through "..._5.19.3_2562.dmg") -
  the same one-driver-line shape as Kyocera, not Canon's genuinely distinct UFRII/PS/PPD split
  or Ricoh's many-small-disjoint-downloads. Like Kyocera, the manufacturer's own name is always
  in the real filename, so a single "Xerox" token unlocks the catalog-driven model index.
- Xerox's real driver package (identifier `com.xerox.drivers.pkg`, install-location `/`) holds
  178 real PPDs (confirmed via `*NickName`) alongside ~6,371 unrelated files (frameworks, print
  filters, PDE plugins, a config-utility app) sharing the same Payload - every real PPD named
  `Xerox <model>.gz`, no `.ppd` anywhere, the same real gotcha Ricoh's legacy bundle had.
  Generalized what was Ricoh-only (`ricohPPDExtractionFallback`) into a manufacturer-agnostic
  `pathFragmentPPDExtractionFallback` (`macppd.go`) rather than duplicating it - Ricoh and
  Xerox now share the same content-based extraction fallback. A real, confirmed-live bonus:
  macOS's own `cpio` silently never writes out the AppleDouble resource-fork sidecar entries
  (`._Xerox <model>.gz`) sharing the same path fragment as the real PPDs, so no extra filtering
  was even needed for those.
- `findOption` (`printdefaults_darwin.go`) now recognizes Xerox's own real ColorModel-equivalent
  keyword, `XROutputColor` (`*OpenUI *XROutputColor/Xerox Black and White: PickOne`) - unlike
  Sharp's own ARCMode, Xerox's own choice values are already self-describing
  ("PrintAsGrayscale"/"PrintAsColor"), so no label-matching gap this time, just the missing
  keyword.
- `planXeroxBatchRow` (`canonbatch_darwin.go`) folds Xerox's own full `installer -pkg` run into
  the same shared 1-auth-prompt batching Ricoh/Sharp already get. Flagged honestly rather than
  claimed as confirmed: unlike Ricoh/Sharp, no real Xerox deploy has run yet, and its own
  package is meaningfully bigger (60MB Payload, 6549 files) than either - closer in scale to
  Canon's own UFR II package that specifically needed selective install to stay fast. Xerox's
  own installer also has no selectable choices to select down even if it does turn out slow
  (unlike Canon/Kyocera) - worth watching the first real deploy's own timing.
- One real, confirmed-but-dormant quirk, documented but not fixed: Xerox's own `*NickName`
  bakes its driver's own version string directly into the name (`"Xerox C300 Color Printer,
  5.19.3"`) - if a technician ever keeps two different Xerox driver versions side by side in
  the same OS-version folder, the same physical model would register as two different friendly
  model names rather than two coexisting variants of one model. No real file demonstrating this
  combination exists yet, so left alone rather than guessed at.

**Confirmed live**: Ken ran a real 2-row Xerox deploy ("zChip Foo"/"zKey Peanut") against the
rebuilt app - exactly 1 elevated prompt for both rows (the full install completed in ~39s
under that one prompt - notably slower than Ricoh/Sharp but still comfortably fast enough that
no selective-install treatment is needed), no ColorModel warning on either row, and CUPS' own
"Xerox Black and White" option (the PPD's own label for `XROutputColor`) showed the correct
value for the row's own intended Mono setting.

## 2026-09-13 (v0.9.6) - macOS: two real Sharp deploy bugs found live, both fixed

Ken's own first real deploy against v0.9.5's Sharp support (2 rows, "zCom Two"/"zCom Three")
found two real bugs immediately - exactly the "confirmed live, not just against synthetic
fixtures" discipline this project's whole macOS effort has followed throughout.

### Fixed
- **3 elevated auth prompts for a 2-row deploy, should be 1.** Sharp had a real catalog-driven
  model index (v0.9.5) but no batched-deploy planner - every row fell through to the old,
  un-batched per-row path, each paying its own separate `osascript` prompt (1 shared install +
  1 queue-create per row = 3 for 2 rows). Added `planSharpBatchRow` (`canonbatch_darwin.go`),
  the same "just fold a plain full `installer -pkg` run into the shared batch" shape
  `planRicohBatchRow` already uses (Sharp's own real package installs in well under 20s - no
  Canon/Kyocera-style selective extraction needed). Generalized what was `RicohPPDPathForDefaults`
  into a manufacturer-agnostic `driver.PPDPathForDefaults` (`macppd.go`) rather than duplicating
  it a second time - Ricoh and Sharp now share the exact same extraction helper.
- **zCom Two's Color Mode stayed "Automatic" instead of the requested Black & White** (zCom
  Three's own PPD is genuinely monochrome-only - confirmed via `*ColorDevice: False`, no bug
  there, matching Ken's own read of it). Root-caused to two layered bugs against Sharp's real
  PPD's own `*OpenUI *ARCMode/Color Mode: PickOne` block:
  1. `findOption`'s exact-keyword allowlist didn't recognize `ARCMode` as a ColorModel-equivalent
     keyword at all (only "ColorModel"/"CNColorMode") - added `"arcmode"`.
  2. Sharp's own real choice *values* are abbreviated, non-self-describing codes ("CMAuto",
     "CMColor", "CMBW") - only each choice's own *label* ("Automatic", "Color", "Black and
     White") is human-readable, and `pickChoice` only ever matched against the raw value.
     Split `ppdOption`'s choices into a new `ppdChoice{value, label}` pair - `value` is still
     exactly what gets sent to `lpadmin -o Key=Value`, but matching (`pickChoice`) now searches
     value *and* label together, so "black" in "Black and White" correctly resolves to `CMBW`.
     `listPPDOptions` (`lpoptions -l`, an already-existing queue) has no way to recover a label
     at all - stays value-only there, same as every manufacturer inspected so far whose real
     values were already self-describing (Canon, Kyocera, Ricoh - unaffected).
  - New regression tests use the real `SHARP BP-20C20.PPD.gz` ARCMode block, copied verbatim.

**Confirmed live**: Ken re-ran a real 2-row Sharp deploy ("zFoo Boo"/"zGah Boo") against the
rebuilt app - exactly 1 elevated prompt for both rows (19s total, both queues configured
immediately after with no further prompts), no ColorModel warning on either row. Both fixes
hold under a real deploy, not just the unit tests.

## 2026-09-13 (v0.9.5) - macOS: real Sharp driver support (147 models)

Ken: "Let's move on to Sharp" - the same "inspect real files first" investigation already applied
to Canon/Kyocera/Ricoh this cycle, extended to Sharp's own real macOS downloads.

### Added
- `macFamilyPreference["Sharp"] = {"MacPS", "PPD"}` (`macfamily.go`) gives Sharp a real
  catalog-driven model index, the same as Kyocera/Ricoh - confirmed live against every real file
  across all 8 OS-version folders in the actual Drivers folder that only two distinct filenames
  ever appear: `MX-C55c_2512a_MacPS.dmg` (the real driver - its own
  `jp.co.sharp.document.mx-c55_1015-.pkg` sub-package holds 147 real Sharp PPDs, confirmed via
  `*NickName`, covering nearly Sharp's whole current BP-/MX- lineup) and
  `Generic_GUC_PrinterSoftware_11202025.dmg` (a Lexmark-licensed, white-labeled generic
  print-dialog-enhancement package - its own `PackageInfo` bundle list references
  `com.lexmark.ColorSeriesProductConfig` - confirmed to hold zero real PPDs, and the exact file
  the old guess-based `ResolveMac` newest-by-mtime fallback was wrongly auto-populating into the
  Driver field, matching Ken's own earlier bug-report screenshot).
  - Unlike Kyocera, Sharp's real driver filename never contains the manufacturer's own name, so
    the established single-token "manufacturer name in filename" trick doesn't apply as-is -
    `"MacPS"` is a real, collision-free substring of the driver's own filename instead, verified
    to never match the decoy's filename.
  - Every one of Sharp's 147 real PPDs' `*NickName` carries a generic, non-language `" PPD"`
    suffix (e.g. `"SHARP MX-3071S PPD"`) rather than a distinguishing driver family - `"PPD"` is
    listed second purely so `stripLanguageSuffix` strips it from the friendly model name (the
    same trick Canon's own real "PPD" family already relies on), giving a clean
    `"SHARP MX-3071S (Driver)"` label instead of a redundant `"SHARP MX-3071S PPD (Driver)"` one.
  - No Japan-market-only convention found in Sharp's real data (unlike Canon's `" JP"` or Ricoh's
    `" JPN "`/glued-`J`) - checked, none present.
  - No custom PPD-extraction fallback or sub-package restrictor needed (unlike Ricoh's
    extensionless PPDs or Kyocera's duplicate sub-packages) - Sharp's real PPDs are consistently
    `.PPD.gz`, found correctly by the existing extension-based `cpio` glob.
- Live-verified: `pdtdebugmac models` against the real Drivers folder correctly indexes all 147
  Sharp models, every variant sourced from `MX-C55c_2512a_MacPS.dmg`, with the decoy package
  never appearing anywhere in the output.

### Fixed
- `TestResolveMacFamily_ManufacturerWithNoFamilyTableBehavesLikeResolveMac` and
  `TestBuildMacModelIndex_ManufacturerWithNoFamilyTableIsAbsent` used "Sharp" as their own example
  of "a manufacturer with no family table at all" - no longer valid now that Sharp has one both
  switched to "HP" instead.

## 2026-09-13 (v0.9.4) - macOS: Apple's own Generic PostScript/PCL drivers as a real fallback

Follows directly from v0.9.3: Ken asked whether an OS-mismatched vendor driver could actually
fail to install or work correctly on the older endpoint (yes, confirmed - a real installer
OS-version check can refuse outright, or worse, a driver that "installs successfully" might
not actually work on that release) - and then proposed the right fix: offer Apple's own
generic drivers, bundled with CUPS itself, as a real, safe, always-available fallback instead
of either silently risking a wrong-OS install or a dead-end "unavailable" state. Scoped
explicitly (Ken, 2026-09-13): only ever offered when no real driver candidate exists at all -
never alongside a real option, never auto-picked.

### Added
- `driver.GenericDriverCandidates`/`GenericDriverModelByLabel` (`macgeneric.go`) - Apple's own
  bundled `drv:///sample.drv/generic.ppd` ("Generic PostScript Printer") and
  `drv:///sample.drv/generpcl.ppd` ("Generic PCL Laser Printer"), confirmed live via
  `lpinfo -m` to be real, valid `-m` model strings CUPS generates a real PPD from on demand -
  part of the OS itself, not a vendor download, so (unlike every other option in this
  codebase) genuinely OS-version-proof.
- `DriverCandidates` (`drivercatalog_darwin.go`) now falls all the way through to these two
  labels only when the catalog-driven, guess-based, *and* OpenPrinting-fallback sources all
  come up empty - the exact scenario v0.9.3's own OS-version filtering can now produce for
  real (a vendor package existed but got correctly excluded for being built for a different
  macOS release).
- `resolveDriver` (`deploy_darwin.go`) recognizes an explicit Generic selection first and
  skips straight to queue creation with a `-m drv:///...` argument - nothing to install, CUPS
  already has it built in. `buildEnsureQueueArgv`/`isGenericModelReference`
  (`queue_darwin.go`) add the third `-m <model>` mode alongside the existing `-P <path>`/
  `-m everywhere`. `PrintDefaultsForNewQueue` skips trying to pre-read a nonexistent PPD file
  for this shape, falling back to reading the real, now-materialized queue's own options live
  after creation - the same path the existing `-m everywhere` case already uses.

### Changed
- **Reversed `filterToCurrentOSVersionFolder`'s own v0.9.3 fallback behavior**: when the
  current OS is known but nothing matches, it now returns empty instead of falling back to
  the unfiltered (OS-mismatched) set. "An OS-mismatched driver beats none" was the wrong
  tradeoff once it's understood installing one carries real risk PDT has no way to verify
  after the fact - a confirmed-empty result is exactly what lets the whole resolution chain
  correctly fall through to the new Generic fallback instead of silently risking a wrong-OS
  install.

### Confirmed live
Found a way to verify significantly more than a unit test alone without needing full
elevation: CUPS's own driver helper daemon generates the real PPD content from a `drv://`
reference directly, unprivileged, no queue creation needed
(`/usr/libexec/cups/daemon/cups-driverd cat drv:///sample.drv/generic.ppd`). Both real
generated PPDs are complete and well-formed (1084/1330 lines, not stubs), with
`*NickName`/`*ModelName` matching this code's own label strings exactly, and both declare a
standard `*Duplex` option with the exact choices (`None`/`DuplexNoTumble`/`DuplexTumble`)
this codebase's existing Duplex-detection logic was already built for - confirmed via a new
test using that *exact* real generated content, copied verbatim
(`TestParsePPDOpenUIOptions_RealGenericPostScriptDuplexBlock`). Neither declares a
`ColorModel`-shaped option at all (a static `ColorDevice: True`/`False` flag instead) - not a
bug, an inherent limitation of Apple's own generic driver definitions: a row's own Mono
checkbox has nothing to act on for either. The one thing this still couldn't verify is the
actual privileged `lpadmin -m drv:///...` queue-creation call itself (same elevation
limitation as `pdtdebugmac installpkg`/`deployqueue`) - low remaining risk, given `-m` is now
confirmed to resolve to a real, well-formed, correctly-labeled PPD via CUPS's own official
mechanism, and `lpadmin -m <model-from-lpinfo-m>` is textbook, documented CUPS behavior.

## 2026-09-13 (v0.9.3) - macOS: driver resolution now filters by the current machine's own OS version

Ken's first real test of v0.9.1's multi-version dropdown surfaced a real, previously-
invisible gap: a Canon UFR II version placed specifically for macOS 10.15 (Catalina) was
showing up as a selectable "coexisting version" on a real macOS 26 (Tahoe) machine, with
nothing distinguishing it as OS-incompatible. Investigated before designing a fix (not
guessing): confirmed this filtering never existed anywhere in the codebase, on either
platform - Windows merges every version folder deliberately (driver rarely genuinely
Windows-version-specific), and macOS's `MacPackage` never even recorded which OS-version
folder a package came from at all. This was always a latent gap; v0.9.0's multi-version
dropdown just made it visible for the first time (a wrong-OS package used to get silently
picked without ever being *shown* as a distinct option).

Ken's own follow-up clarification made clear this isn't just a dropdown cosmetic fix: PDT
travels on a synced flash drive from a technician's own laptop to whichever client endpoint
it gets plugged into next, which may be on an older macOS release than the laptop that built
the catalog - the right driver to use is always whichever matches the machine PDT is
*actually running on at that moment*, not the machine that built the catalog.

### Added
- `MacPackage.OSVersionFolder` - the immediate Drivers/macOS/\<Manufacturer\>/ subfolder a
  package was found under (e.g. "26-Tahoe", "10.15-Catalina"), captured once at scan time.
- `filterToCurrentOSVersionFolder` (`internal/driver/macosversion.go`) - queries the real,
  actual running machine's own macOS version live via `sw_vers` (cached once per process,
  never persisted or assumed from a different machine) and narrows a package list down to
  just the current release's own compatible ones. Parses only the *leading version number*
  out of a folder name (not a hardcoded codename table - Apple ships a new one yearly, and
  this project's own scaffold code already rejected hardcoding that list for exactly this
  reason) - "26-Tahoe" -> "26", "10.15-Catalina" -> "10.15". Falls back to the full,
  unfiltered set whenever filtering can't be done with real confidence: the current OS
  version couldn't be determined at all, filtering would leave zero packages, or a package's
  own folder name doesn't match the recognized convention - never a hard failure, matching
  this codebase's own "best-effort, degrade gracefully" philosophy throughout.
- Applied to both real driver-resolution paths, not just the new multi-version dropdown:
  `packagesInFamily`/`newestInFamily` (catalog-driven and guess-based family resolution) and
  `ResolveMac` (the plain single-package guess-based path every non-cataloged manufacturer
  still uses) all share the same `familyCandidates`/`filterToCurrentOSVersionFolder` step now.

### Confirmed live
Rebuilt the real Canon catalog on this machine (macOS 26/Tahoe): the Driver dropdown now
shows only the 26-Tahoe-appropriate versions (UFR II v10.19.25, PostScript v4.17.24, Generic
PPD v5.50) - the older 10.15-Catalina/10.14-Mojave/10.13-HighSierra-specific versions
(UFR II v10.19.21, PS v4.17.22/v4.17.20, PPD v5.35/v5.25) are correctly excluded, since this
machine's own Drivers folder never had copies of them placed in the 26-Tahoe folder to begin
with. Kyocera and Ricoh rebuilt cleanly alongside it with no regressions. Existing tests
updated to deterministically disable this filtering where it isn't the thing being tested
(`disableOSVersionFiltering`) rather than depending on whichever real macOS version happens
to run the test suite - new dedicated tests cover the filtering/fallback logic itself.

## 2026-09-13 (v0.9.2) - macOS: catalog.<mfg>.json now prunes a fully-removed/archived package

Closes out issue #5, deliberately deferred until #4 (multiple coexisting driver versions)
shipped, since #4 changed what "stale" even means here.

### Fixed
- **`catalog.<mfg>.json` never cleaned up after a fully-removed/archived package.**
  Confirmed live (originally, filing #5): moving a real Ricoh package into an `Archive`
  subfolder (or deleting it outright) correctly dropped it from the *live, in-memory* model
  index - `packagesInFamily` returning zero packages for that family meant nothing re-added
  its entries to `byModel` - but the *persisted* catalog file was left completely untouched,
  since `BuildMacModelIndex`'s own `len(all) == 0` branch `continue`d straight past the
  pruning logic every time, never reaching it. Worse, if *every* family for a manufacturer
  disappeared at once (the whole `Drivers/macOS/<Manufacturer>` folder removed), `dirty`
  never became `true` for any family, so the entire catalog file would have survived on disk
  forever, fully stale.
- Fixed by giving the `len(all) == 0` branch the same pruning treatment a real content change
  already gets: `DiffModels` against an empty "current" map correctly reports every model the
  family previously had as removed (reusing the exact diff mechanism a real content change
  already produces, logged the same way), `cat.Models`'s own entries for that family are
  dropped, and both `cat.Provenance[family]`/`cat.ExtraProvenance[family]` are deleted
  outright - `dirty` is set, so the file actually gets rewritten.

### Confirmed live
Replayed #5's own original reproduction exactly: archived the same real Ricoh package
(`Ricoh_IM_C300_C400_LIO_1.5.0.0.dmg`) across every OS-version folder again and rebuilt -
this time the persisted `catalog.ricoh.json` correctly dropped from 354 to 351 models (its
own 3 - "RICOH IM C300/C400/C400SR PS" - gone, confirmed by exact name match, not just a
substring check that would have also matched unrelated legacy models like "RICOH Aficio MP
C400"), and its own `IM_C300_C400` provenance entry removed entirely. Restored the file
afterward. New regression test:
`TestBuildMacModelIndex_PrunesFamilyThatDisappearedEntirely`.

## 2026-09-13 (v0.9.1) - macOS: fix v0.9.0 wiping out Kyocera/Ricoh's Model dropdown

Ken's first real test of v0.9.0 found Canon working, but Kyocera and Ricoh had lost their
Model dropdown entirely - the Driver field just auto-populated with a raw package label
(the pre-catalog guess-based fallback), as if neither manufacturer had ever had catalog
support at all.

### Fixed
- **A real migration bug**: every catalog.\<mfg\>.json written before v0.9.0 (i.e. every
  real one Ken had) has each entry's own new `SourcePackagePath` field empty, since that
  field didn't exist yet when those files were written. The new per-package cache lookup
  (`ModelsForFamilyPackage`) matches on that field, so it came back empty for every
  pre-v0.9.0 entry - and `cachedVariantFilesExist`'s own vacuous-true-on-an-empty-map
  behavior meant that empty result was silently trusted as "already correctly cached,
  nothing to do" instead of falling through to a real reindex. Kyocera and Ricoh's real
  packages were completely untouched; the catalog just never looked at them again. Fixed
  by requiring the cache lookup to actually return something before trusting it, and by
  having the reindex that follows correctly replace - not just add alongside - any
  leftover pre-v0.9.0 entries for the package currently being (re)indexed.
- **A second, related bug found immediately after fixing the first**: the established
  Drivers/macOS/\<Manufacturer\>/\<OS-version\>/... convention has a technician copy the
  same download into several OS-version folders (a real Kyocera "Web Build" download
  showed up in 10 folders) - packagesInFamily's own (v0.9.0) deduplication correctly
  collapses those into one representative package, but the *other* 9 copies, each
  individually tracked as their own "package" before that deduplication existed, were
  never revisited by the per-package loop again once they dropped out of the current set
  - their own stale catalog entries lingered forever. Every real Kyocera model was showing
  10 duplicate "versions" of the identical download. Fixed with an explicit cleanup pass
  that prunes any catalog entry or provenance record for a package no longer part of a
  family's current package set at all, once that family's own current packages have all
  been processed.

Both bugs are new regression tests (`TestBuildMacModelIndex_MigratesLegacyCatalogMissingSourcePackagePath`,
`TestBuildMacModelIndex_PrunesOrphanedPackageNoLongerInCurrentSet`), and both confirmed
live against Ken's own real, previously-broken Kyocera and Ricoh catalog files (not
synthetic fixtures) - both restored to their correct model counts (460 and 354
respectively) with zero duplicate entries.

## 2026-09-13 (v0.9.0) - macOS: multiple coexisting driver versions, individually selectable

Ken asked whether two versions of the same manufacturer's driver could coexist in the
Drivers folder the way Windows' own Kyocera handling already allows (`Candidates()`'s
per-version decorated Driver-dropdown labels) - checked the real code and found macOS had
no equivalent: `newestInFamily` always collapsed straight to the single newest package,
silently discarding any older one a technician might have deliberately kept around.

### Added
- **Every compatible package for a family gets indexed now, not just the newest**
  (`packagesInFamily`, replacing `newestInFamily` inside `BuildMacModelIndex`) - a
  technician can now deliberately hold a printer fleet back on an already-validated older
  driver version and still have it show up as its own selectable Driver-dropdown entry,
  real parity with Windows' own multi-version `Candidates()` decoration.
- **Each coexisting package gets its own independent staleness cache**
  (`MacManufacturerCatalog.ExtraProvenance`, `IsCurrentForPackage`,
  `ModelsForFamilyPackage` - additive to the existing catalog.\<mfg\>.json schema, so
  already-written catalog files keep working unmodified) - an older, rarely-changing kept
  version doesn't get re-inspected on every launch just because it isn't the newest one.
- **A model's Driver-dropdown label only gets decorated once a real second version
  exists** (`decorateMultiVersionLabels`) - a single-version model's label stays exactly
  as plain as it always was. Decorated with the bare filename + the file's own
  modification date (`packageVersionTag`) - not `PackageLabel`, whose first attempt is a
  real, confirmed-expensive `pkgutil --expand-full` call; not a real declared version
  field either, since macOS installer packages don't reliably carry one at all (confirmed
  against real Canon/Kyocera/Ricoh downloads - see `ResolveMac`'s own doc comment).
- **A blank/ambiguous Driver selection always still resolves to the newest version** -
  Ken's own explicit requirement. `packagesInFamily` returns newest-first, so every
  downstream consumer (`MacVariantForDeploy`, `MacModelCandidates`) already gets this for
  free from append order, no extra logic needed.
- Two coexisting versions that happen to share a no-installer/loose-PPD family's own PPD
  filename no longer silently overwrite each other's permanently-cached copy
  (`packageCacheKey` gives each package its own cache subdirectory).

### Fixed
- A real nil-map panic caught by this work's own new tests before it ever shipped:
  `LoadMacManufacturerCatalog`'s "empty catalog" value never initialized the new
  `ExtraProvenance` field, so a technician's very first multi-version build (nothing on
  disk yet) would have crashed outright.
- A real cache-reuse bug for the no-installer/loose-PPD family shape (Canon's own "PPD"
  bucket): the new per-package cache lookup matched on `PackagePath`, which is
  deliberately left empty for that shape (deploy_darwin.go reads that emptiness to mean
  "no `installer` run needed") - a cache-hit rebuild silently dropped that family's own
  variants. Fixed with a new `SourcePackagePath` field, always set regardless of shape,
  used for cache/pruning matching instead of repurposing `PackagePath`'s own existing
  meaning.
- **A real, more serious bug found only by live-testing against production data, not
  synthetic fixtures**: the established Drivers/macOS/\<Manufacturer\>/\<OS-version\>/...
  convention has a technician copy the *same* downloaded file into several OS-version
  folders side by side - confirmed live that a real Canon UFR II download sitting
  identically in 6 different OS-version folders was, before this fix, surfaced as 6
  separate "coexisting versions" in the Driver dropdown. `packagesInFamily` now
  deduplicates by (basename, size) before treating anything as a distinct version -
  the same "never silently modified in place" assumption `IsCurrent`'s own staleness
  check already relies on.

### Confirmed live
Staged a real second Canon UFR II version (a genuinely different real download,
`UFRII_v10.19.23_mac.dmg`) alongside the two already present across the Drivers folder
(`UFRII_v10.19.25_mac.dmg`, `UFRII_v10.19.21_mac.dmg`) and rebuilt the real catalog:
all 3 versions correctly indexed and decorated, newest-first, no duplicate/corrupted
entries, catalog file restored cleanly afterward. Also surfaced (and filed as its own,
separate, lower-urgency issue - not part of this work's scope) a real, pre-existing
locale-handling inconsistency between different Canon "PPD" bucket versions, invisible
until an older package was ever indexed at all.

## 2026-09-13 (v0.8.0) - macOS: Ricoh driver support (catalog, install, batching)

Ken's next real, second-manufacturer test of the "generalize past Canon" work
(issue #2) - Ricoh's own real shape turned out meaningfully different from both
Canon and Kyocera: many small, independent downloads side by side (10 real files
inspected), each covering its own small, disjoint model group, rather than one
driver line periodically superseded.

### Added
- A real catalog-driven model index for Ricoh (`macricoh.go`) - 427 real models
  across 10 real downloads, each download's own family token a verified
  collision-free filename fragment. Confirmed live that a download's own filename
  systematically undersells its real model coverage (a "2500/3500/4000"-named
  file actually registers 9 real models).
- **A real, latent extraction bug found and fixed**: Ricoh's own modern PPDs
  carry no file extension at all, and one legacy Apple-distributed bundle
  (`RicohPrinterDrivers.pkg`, 356 real models, Snow Leopard-era) names them
  `<model>.gz` with no ".ppd" anywhere - neither matched the existing
  extension-based cpio glob at all. Added a manufacturer-dispatched, content-
  verified fallback (`ppdExtractionFallback`, `looksLikeRealPPD`) - zero cost or
  behavior change for Canon/Kyocera, whose real PPDs still match the fast path.
- Ricoh's own real packages are small (~150x smaller than Canon's) - a full
  `installer -pkg` run is already fast (confirmed live: ~20s including the real
  auth wait), so no Canon/Kyocera-style selective installer was needed, just a
  plain full install (`planRicohBatchRow`) folded into the existing 1-auth-
  prompt batching.
- Two more real Japan-market-only naming conventions found and filtered
  (`isJapanMarketOnly`), neither matching Canon's own "trailing ` JP`" shape -
  an explicit `JPN` token before the language suffix, and a bare `J` glued onto
  the model number itself.

### Confirmed live
Real deploys of two different Ricoh models batched to 1 auth prompt; full
install completes in the confirmed ~20s range.

## 2026-09-12 (v0.7.2) - macOS: extend the 1-auth-prompt batching to Kyocera

Ken confirmed v0.7.1's Kyocera install itself now works correctly and completes quickly, but still
cost 4 separate native auth prompts for a 2-row deploy - the "batch every row into one elevated
call" work from v0.6.9 only recognized Canon UFR II-shaped packages, so every Kyocera row was
still taking the old, unbatched, 2-prompts-per-row path.

### Added
- **`PrepareBatch` now recognizes Kyocera rows too**, not just Canon. Refactored into a
  manufacturer-agnostic core (`canonBatchRowPlan.installScript` - a fully-rendered "install the
  manufacturer's own real components, then place this row's own PPD" shell fragment) plus two
  separate planners (`planCanonBatchRow`, `planKyoceraBatchRow`) that each build one using exactly
  the same mechanics their own non-batched `installCanonSelective`/`installKyoceraSelective`
  already use - just returning a string instead of running it immediately. Whichever planner
  recognizes a row's actual package shape claims it; everything else (a different manufacturer, the
  guess-based fallback, a loose-PPD family, or an existing queue) still falls back to the old
  per-row path unaffected. Confirmed live: 1 auth prompt for a 2-row same-package Canon deploy
  already proved the underlying mechanism; this extends the same mechanism to a second
  manufacturer's own real package shape.
- The per-run "shared components already installed" cache (`sharedComponentsInstalledThisRun`) is
  now correctly updated after a batched run too, not just after the old non-batched path - a later
  row needing the same package (batched or not) never pays to reinstall it twice.

Also confirmed live and not a bug: a genuinely monochrome-only Kyocera model (`ECOSYS MA4500ifx`,
`*ColorDevice: False`) correctly warns "no ColorModel option" - its only "Color"-named PPD option
(`*WmColor`) is for watermark tint, unrelated to print color mode.

**Confirmed live**: a 2-row same-package Kyocera deploy (`TASKalfa 2550ci` + `TASKalfa 6052ci`)
completed both rows' install+queue-create within the same second after a single wait for the auth
prompt - the batched path, not the old 2-prompts-per-row fallback.

## 2026-09-12 (v0.7.1) - macOS: fix v0.7.0's Kyocera install (one sub-package can't install under this elevation mechanism)

Found live testing v0.7.0's own selective Kyocera install: both rows failed with `PKInstallErrorDomain
Code=120 "An unexpected error occurred while moving files to the final destination."`, after 6 of
16 sub-packages had already installed successfully.

### Fixed
- **"Print Panel App" (a GUI status/monitoring utility) is the only one of Kyocera's 19 choices
  whose own `PackageInfo` declares `install-location="/Applications"`** - every other real choice
  targets `/Library/...`, `/usr/libexec/cups/filter`, or `/Library/PreferencePanes`. Got real
  verbose installer output (`installer -verboseR -dumplog`, run directly rather than guessed at) to
  find the actual underlying error: `NSPOSIXErrorDomain Code=1 "Operation not permitted"` inside
  PackageKit's own sandboxed install, specifically while moving files into `/Applications` - not
  something a script-side workaround like `-X` or `--flatten` can fix, since the failure is inside
  `installer`'s own internal move, not anything PDT's own command controls. Very likely a TCC/SIP-
  related restriction on this specific elevation mechanism that a real, GUI-driven `Installer.app`
  run wouldn't hit. Skipped entirely rather than risked further - a GUI status app isn't required
  for actual CUPS printing (the real functional pieces - CUPS filters, PDEs, the driver Framework -
  all target `/Library`/`/usr` and already installed successfully).

**Not yet re-verified with a real deploy** - confirmed via `pkgutil`/direct inspection that Print
Panel App is now correctly excluded from the "install for real" list (15 remaining, down from 16),
and confirmed all 15 still flatten successfully (non-privileged). A live re-test of the full
privileged install is still needed - two attempts to self-verify this round went unanswered rather
than actually failing (no error, no partial state left behind either).

## 2026-09-12 (v0.7.0) - macOS: generalize the model catalog + selective install past Canon (Kyocera)

Ken reported a real Kyocera install taking 3-5+ minutes and asked for "a custom installer like we
did for Canon" - the first real second-manufacturer case for the "generalize past Canon" work
noted as a future item throughout this project's own history (tracked in
[github.com/keteague/PDT/issues/2](https://github.com/keteague/PDT/issues/2), now addressed for
Kyocera specifically).

### Added
- **Kyocera now gets a real, catalog-driven model index**, the same as Canon - previously Kyocera
  had no `macFamilyPreference` entry at all, so Model resolved only through the older guess-based
  install-then-diff-then-fuzzy-match fallback. `internal/driver/mackyocera.go` parses a Kyocera
  "Web Build" Distribution's own `<pkg-ref>` bundle identifiers (far more stable across different
  downloads than file names - Kyocera's own web-based driver-builder tool names the outer
  `.dmg`/`.pkg` after the build date) to identify its baseline PPD-only sub-package.
- **Selective Kyocera install** (`internal/printer/darwin/kyoceraselective_darwin.go`,
  `installCanonSelective`'s own sibling) - installs the 16 real functional sub-packages (driver
  framework/CUPS filters/PDEs/Print Panel App/etc) for real, then `cpio`-extracts just the *one*
  target model's own PPD out of the baseline installer's Payload - never running the "Duplex On"
  or "Net Manager On" choices' own installer at all. Both were confirmed to ship the exact
  byte-identical PPD set as the baseline, differing only in a default value each one's own
  postinstall script patches in afterward via a slow per-file `sed`+`cp` shell loop (which itself
  also re-scans every PPD already installed on the machine looking for duplicate device IDs to
  remove first) - since PDT already sets duplex/color defaults itself via `lpadmin` after queue
  creation, neither patched variant was ever useful here.
- Extended `extractPPDsFromExpandedPkg`/`packagePPDEntries` with an optional sub-package
  allow-list (`extractPPDsFromExpandedPkgFiltered`/`packagePPDEntriesFiltered`) so Kyocera's own
  catalog indexing can restrict itself to just the one real PPD source, never the two redundant
  variants - without this, every Kyocera model would have been indexed 3 times over with
  completely duplicate entries.

### Fixed
- **A real, shared-code bug found investigating the above**: `extractPPDsFromExpandedPkg`'s own
  `cpio` extraction pattern only matched lowercase `*.ppd`/`*.ppd.gz` - `cpio`'s glob matching is
  case-sensitive, and a real Kyocera download mixes both extension cases in the very same
  sub-package (confirmed live: 124 real PPDs named `*.ppd`, 336 named `*.PPD`). This silently
  dropped 73% of Kyocera's own real model coverage from the catalog - caught only because the
  indexed model count (124) came back suspiciously low against the real BOM's own file count
  (460), not from any error or warning. Now matches all four case combinations
  (`*.ppd`/`*.PPD`/`*.ppd.gz`/`*.PPD.gz`); confirmed live the full 460-model set now indexes
  correctly. This was latent, not previously triggered, for every other manufacturer inspected so
  far (Canon's own real downloads happen to use consistent lowercase extensions throughout).

### Testing
- `TestExtractPPDsFromExpandedPkg_CaseInsensitiveExtensions` (new) - builds a real gzip-compressed
  cpio archive via the system `cpio`/`gzip` tools directly (no synthetic byte-format guessing) and
  confirms both a lowercase- and uppercase-extension PPD get extracted; confirmed this test
  actually fails without the fix by temporarily reverting it.
- `TestResolveMacFamily_ManufacturerWithNoFamilyTableBehavesLikeResolveMac`,
  `TestBuildMacModelIndex_ManufacturerWithNoFamilyTableIsAbsent`: updated to use "Ricoh" instead of
  "Kyocera" as the "manufacturer with no family table" example, since Kyocera now has one.

**Not yet verified with a real deploy** - static/non-privileged verification only this round: the
real Distribution XML parsing, sub-package identification, PPD extraction, and all 16 sub-package
flattens were each confirmed against the real downloaded Kyocera package (`pkgutil`/`cpio`, no
root needed for any of it), and the full 460-model catalog now builds and caches correctly. The
actual privileged install path (`installer -pkg` × 16 + the PPD copy, all in one elevated call)
has not yet been exercised live - needs a real Kyocera deploy to confirm end to end.

## 2026-09-12 (v0.6.9) - macOS: batch every Canon row's privileged work into one elevated call per deploy run

Ken asked explicitly for this after a 2-row deploy still cost 4 separate native auth prompts (1
install + 1 queue-create per row) - confirmed, again, that `do shell script ... with administrator
privileges` never reuses a recent grant, so the only way down to fewer prompts is fewer actual
invocations of it.

### Added
- **`printer.BatchPreparer`** - an optional `Deployer` extension (`PrepareBatch(ctx, reqs, confirm)`),
  checked via a type assertion in `DeployAllWithProgress` before any row's own `Deploy()` call.
  Windows' own Deployer doesn't implement it, so this is a no-op there.
- **macOS's own `PrepareBatch`** (`canonbatch_darwin.go`) - a first, entirely unprivileged pass over
  every row: resolves each one's catalog-driven Canon UFR II variant, expands/flattens its package,
  extracts its own staged PPD+Recipe files, computes its queue name/device URI/print-defaults args -
  everything `installCanonSelective`/`Deploy()` already knew how to do, just not executed yet.
  Every *batchable* row's own install+queue-create+defaults commands are combined into **one**
  script and run through **one** elevated call for the whole deploy run, deduplicating each unique
  package's own Core install exactly the way the existing per-row cache already did. Per-row
  success/failure comes back through a plain results file (`printf '%d:%d\n' <rowIndex> $? >>
  file`, one line appended per row's own subshell) rather than by parsing the combined call's own
  stdout - confirmed this session (twice) that `do shell script` mangles `\n` to `\r` and buffers
  everything until the whole script exits, both real problems for structured multi-row output a
  results file sidesteps entirely by having Go read it straight off disk afterward.
  Each row's own commands run inside `( ... )` so one row's failure can never block another's -
  confirmed live (non-privileged) with a synthetic 3-row script including a deliberately-failing
  middle row: the other two still completed and each row's own real exit code came back correctly.
- **Deliberately narrow scope**: only a row that resolves to a catalog-driven Canon UFR II package
  *and* doesn't already have an existing queue to reuse gets batched - every other case (a
  different manufacturer, the guess-based fallback with no catalog entry, a loose-PPD no-installer
  family, or an existing queue) is left completely alone, falling back to the exact same per-row
  path (and its own separate prompts) unchanged. This is the one path proven correct end to end
  across this session's last several rounds, and covers every row actually live-tested so far.
- The real, accepted trade-off: every batched row's result becomes known only once the one combined
  call returns, not streamed in as each row would otherwise finish - deliberate, not an oversight,
  given the alternative is a fresh native password prompt for every row needing its own privileged
  step.

**Not yet verified with a real deploy** - static verification only this round: the actual generated
script (real paths, real quoting) syntax-checked clean, and the subshell/results-file fault-
isolation mechanism was confirmed correct with a real (non-privileged) test. A live self-test of the
real privileged path was attempted 3 times and each attempt's own auth prompt went unanswered
(no stuck process found afterward, and no test queue was left behind either) - abandoned rather than
keep prompting; needs a real deploy to confirm end to end.

## 2026-09-12 (v0.6.8) - macOS: fix a real color-mode mismatch bug (`findOption`'s suffix match was too loose)

Found live: a fully successful deploy still left one queue (`zBack_Yard`) set to color instead of
mono, with the log warning about `CNProcessColorMode` having no grayscale/mono choice.

### Fixed
- **`findOption`'s suffix match could grab the wrong option entirely** - a real Canon PPD declares
  both `*CNColorMode` (the real color/mono switch, choices mono/color) and `*CNProcessColorMode`
  (an unrelated boolean, "Print Mixed Color/B&W Documents at High Speed", choices False/True), and
  both end in "ColorMode". The suffix match (added in v0.6.2 to catch Canon's "CN"-prefixed
  variants) picked whichever came first in this particular PPD's own option order
  (`CNProcessColorMode`) instead of the real one - correctly finding no mono/gray choice on the
  *wrong* option and silently leaving the real `CNColorMode` at its default. Replaced with an exact
  (case-insensitive) match against an explicit allowlist of known real-world spellings - the
  CUPS-standard `Duplex`/`ColorModel`, and Canon's own `CNDuplex`/`CNColorMode` - rather than any
  suffix or substring match, which can never again collide with an option that merely *contains*
  one of these words.

### Testing
- `TestFindOption_DoesNotMatchCNProcessColorModeInsteadOfCNColorMode` (new, reproduces the exact
  real option ordering that caused this). Existing suffix-based tests renamed/updated to the new
  exact-match calling convention (`"colormode"` → `"cncolormode"`, `"duplex"` alone →
  `"duplex", "cnduplex"`).

## 2026-09-12 (v0.6.7) - macOS: fix v0.6.6's `cp -R` (extended-attribute copy rejected on /Library)

Found live testing v0.6.6's own flatten fix against two more real models: both failed with
`cp: .../Library/.: unable to copy extended attributes to /Library/.: Operation not permitted`,
even running as root through the elevated call.

### Fixed
- **A plain `cp -R src/. /Library/` also tries to copy the *source directory's own* extended
  attributes onto the destination directory entry itself** (not just recurse into its contents) -
  and `/Library`'s own inode metadata rejects that even for root. Confirmed live: reproduced the
  identical failure against the real `/Library` with a harmless, immediately-cleaned-up test file
  (not the real deploy) before fixing it, then confirmed `cp -RX` (`-X`: don't copy extended
  attributes) succeeds where plain `cp -R` failed, same real test. None of the freshly-`cpio`-
  extracted driver files carry attributes worth preserving anyway - the `chown -Rh`/`chmod` calls
  right after already re-establish the ownership/permissions that actually matter.

## 2026-09-12 (v0.6.6) - macOS: fix v0.6.5's Core install (`installer` rejects an expanded sub-package directory)

Found live testing v0.6.5's own selective install against a third real model: install failed
outright with `installer: Error - the package path specified was invalid`, while a second row in
the same run (a no-installer "PPD"-family model) deployed fine - confirming the bug was specific to
the new Core-install path, not a regression elsewhere. Ken's own hypothesis ("it may be a folder,
not a file") pointed at exactly the right place.

### Fixed
- **`installer -pkg` rejects a `pkgutil --expand`-produced sub-package directory outright** -
  reproduced the exact same error running it as the current user with no privileges at all,
  confirming it's a format/recognition issue, not a permissions one (modern macOS `installer`
  apparently no longer accepts a bare expanded-component directory the way older documentation/
  habits suggested). Fixed by re-flattening the expanded Core sub-package back into a proper
  single-file `.pkg` via `pkgutil --flatten` before ever calling `installer` on it - confirmed live
  (still as a non-privileged dry run) that `installer` then correctly reports "Must be run as root"
  instead of rejecting the path, and reproduced the exact previously-failing model
  (`CNPZUIF1643FZU`, "Canon imageFORCE 1643F/1643") end to end through the real Go code to confirm
  the fix before asking for another live retest.
- The flatten step only runs when Core actually needs installing this call (skipped entirely once
  `canonCoreInstalledThisRun` already covers the package, same as before).

Confirmed live in the same test: deploy time was already meaningfully shorter for the row that
*did* succeed, and mono/simplex print defaults are still correctly applied through the new install
path (v0.6.2's fix holding up under the new code).

## 2026-09-12 (v0.6.5) - macOS: real Canon UFR II install speedup (skip ~548 unused PPD/Recipe pairs + 3 unneeded sub-packages)

Ken's own live install-phase timing (v0.6.4) turned out to be fundamentally unmeasurable through
`do shell script` (confirmed via a fast synthetic diagnostic: identical timestamps for lines with
real 1-second gaps between them, even with no intermediate buffering stage at all) - abandoned
rather than chasing a fourth broken attempt. Independently, Ken's own manual installer run
(1 Touch ID prompt) measured file-writing at ~30s and package scripts at ~2min for the *whole*
5-package Distribution, and static inspection (`lsbom`, `nm -u`, reading every sub-package's own
pre/postinstall script) had already established, without needing precise timing, that the real
waste is structural: a UFR II Distribution installs 5 sub-packages (22,900 files, ~224MB) for a
single printer, of which only the Core sub-package (the actual driver framework/backend/filters)
and *one* PPD+Recipe pair out of Device's 549 are ever used.

### Removed
- The v0.6.2-v0.6.4 install-phase timing feature (`installVerboseTimestamped`/`logInstallPhases`/
  `installerPhaseLineRe`) - `do shell script`'s own privileged-execution mechanism buffers a
  command's entire output until it fully exits, no matter how many pipe stages run inside the
  script; there is no way to get genuine incremental timing out of it. Documented both real
  findings from the investigation (universal \n→\r conversion on any `do shell script` return
  value; the buffering itself) directly on `runPrivileged`/`runPrivilegedShell`
  (`elevate_darwin.go`) so a future real-time-progress attempt starts from a fundamentally
  different mechanism (e.g. an elevated script writing to a file an unprivileged goroutine tails
  independently) instead of repeating this dead end.

### Added
- **Selective Canon UFR II install** (`internal/driver/maccanonselective.go`,
  `internal/printer/darwin/canoninstall_darwin.go`) - `installVariant`'s new fast path for a Canon
  UFR II-shaped Distribution: installs the Core sub-package for real (needed - the actual driver
  framework/backend/PDE filter binaries), then selectively `cpio`-extracts just the *one* target
  model's own PPD + matching per-model "Recipe" bundle (confirmed against two real models - the
  PPD, the whole bundle tree, and a sibling `Recipe/<model>.rcp` symlink pointing into it, found via
  a real BOM diff, not a guess) straight out of the Device sub-package's own Payload - never running
  Device.pkg's own installer at all, and never touching Icons/Profiles/cnaccm (confirmed their own
  pre/postinstall scripts are all no-ops on modern macOS, and none are required for functional
  printing - finishing features like staple/punch/fold are declared directly in the PPD's own
  `*OpenUI` options, confirmed live, not dependent on the Recipe bundle). Both the privileged Core
  install and the staged-file placement (`cp`/`chown -Rh`/`chmod`) ride in one combined
  `runPrivilegedShell` call - no extra elevation prompt over the old approach. Falls back to the
  old full-Distribution install unchanged whenever the package doesn't match this expected shape (a
  different manufacturer, or an unexpected/future Canon layout) - never a hard failure just because
  the optimization doesn't apply. Verified end-to-end (mount → expand → locate sub-packages →
  selective extract) against a real UFR II download for two different real models before wiring
  this in, and the `cp -R` merge step against a synthetic tree to confirm it doesn't clobber
  unrelated pre-existing files.
- `Deployer.canonCoreInstalledThisRun` - the Core sub-package's own per-run "already installed"
  cache (bool-valued, separate from the existing full-install `installedThisRun` PPD-list cache) -
  two rows resolving to *different* Canon models sharing one package (the exact real case tested
  live) share one Core install; each row's own PPD+Recipe placement is cheap enough it's never
  cached/skipped, just always run.

### Testing
- `TestCanonCoreDevicePackages_FindsBothByRealNamingConvention`,
  `TestCanonCoreDevicePackages_MissingEitherSubPackageIsNotOK`,
  `TestCanonCoreDevicePackages_MissingDirReturnsNotOK`, `TestCanonPPDBaseName_StripsRealExtensions`.

## 2026-09-12 (v0.6.4) - macOS: fix the v0.6.3 install-phase timing itself (it logged nothing); add a Log context menu

Found immediately live-testing v0.6.3's own fix: a real Canon deploy logged *zero* phase lines
instead of wrong ones - worse visibility than v0.6.2's broken-but-present output.

### Fixed
- **`logInstallPhases` was splitting captured output on `\n` alone, but `do shell script` itself
  silently converts every `\n` in a privileged command's captured stdout to `\r` before handing it
  back** - confirmed via raw byte inspection (`od -c`) of a real `do shell script ... with
  administrator privileges` return value. This is universal AppleScript behavior (a classic-Mac \r
  line-ending convention), not anything specific to `installer -verboseR`'s own output - v0.6.3's
  internal `tr '\r' '\n'` step ran *before* this conversion, so its carefully-created `\n`s got
  turned right back into `\r` on the way out, leaving zero real `\n` characters for
  `strings.Split(out, "\n")` to find. Now splits on `\r` as well (`strings.FieldsFunc`). Documented
  this on `runPrivileged`/`runPrivilegedShell` directly (`elevate_darwin.go`) since it applies to
  *any* future caller parsing multi-line output from either function, not just this one.
- Diagnosed live via a fast, harmless synthetic `do shell script ... with administrator privileges`
  test (a 3-second `sleep`-separated loop, no real system changes) rather than requiring another
  multi-minute real install cycle to isolate.

### Added
- **Log area right-click context menu** - Select All, Copy, Clear Log (a separator between Copy and
  Clear Log). Copy uses the async Clipboard API with an `execCommand('copy')` fallback for a
  restricted webview context. Select All/Copy always act on the full log text (all of
  `state.logLines`), not whatever happened to be selected when the menu was opened - closer to
  what a technician pasting a log into a support ticket actually wants.

### Testing
- `TestLogInstallPhases_HandlesRealCarriageReturnOnlyOutputFromDoShellScript` (new, `\r`-only input
  matching the real captured shape byte-for-byte).
- `joinLines` test helper and all existing `logInstallPhases` fixtures switched to `\r`-joined
  input to match reality.

## 2026-09-12 (v0.6.3) - macOS: fix the v0.6.2 install-phase timing itself (it was measuring the wrong thing)

Found immediately live-testing v0.6.2's own new install-phase timing feature: a real Canon deploy
logged `Install phase "..." took 0s` for a phase that plainly took several minutes, and no log
lines appeared at all during the wait, contradicting the feature's own purpose.

### Fixed
- **`installVerboseTimestamped` was capturing `installer -verboseR`'s output to a file, then
  timestamping it in a replay loop *after* `installer` had already fully exited** - every
  timestamp reflected how fast the replay could read the file back (near-instant), not when
  `installer` actually emitted each line. Confirmed live: a real 5+ minute install logged every
  phase as "took 0s". Rewritten to pipe `installer`'s output *live* through the timestamping loop
  while it's still running (a real concurrent pipeline, not capture-then-replay) - `installer`'s
  own exit code is still captured correctly via `${PIPESTATUS[0]}` (confirmed `do shell script`
  really does run via bash, which supports it) rather than the pipeline's last stage.
- **`-verboseR`'s own progress lines are `\r`-separated** (it renders as a single self-overwriting
  progress line in a terminal), not `\n`-separated - a plain `read -r line` only splits on `\n`, so
  the entire run's output was collapsing into one unparseable blob. Now piped through `tr '\r'
  '\n'` first.
- **The same phase gets re-announced dozens to hundreds of times** as its own internal percentage
  climbs (confirmed live: `Running package scripts…` alone appeared 300+ times in one real
  install) - `logInstallPhases` now only logs a transition when the phase text actually *changes*,
  not on every repeat.
- Corrected `installerPhaseLineRe` to match `installer`'s real line shape
  (`installer:PHASE:<text>`), not the guessed `installer:<text>` shape v0.6.2 shipped with.

### Testing
- `TestLogInstallPhases_DoesNotRelogTheSamePhaseOnEveryRepeatedAnnouncement` (new).
- `TestLogInstallPhases_ComputesElapsedSecondsPerPhaseIncludingTheLastOne` updated to the real
  `installer:PHASE:` line shape and real phase-repeat pattern.

## 2026-09-12 (v0.6.2) - macOS: fix print defaults, cut a redundant auth prompt, add install-phase timing

Found live testing 2 real Canon deploys with mono/simplex defaults, right after v0.6.1 shipped.

### Fixed
- **Duplex/color defaults silently never applied to a real Canon UFR II queue** - `SetPrintDefaults`
  only matched an option keyword *exactly equal to* `Duplex`/`ColorModel`, but Canon's own PPD
  declares `*CNDuplex` (choices `None`/`DuplexFront`/`Booklet`) and `*CNColorMode` (choices
  `mono`/`color`) instead - confirmed against a real installed Canon PPD
  (`CNPZUIRAC5840ZU.ppd.gz`). `findOption` now matches by keyword *suffix*, checked against every
  other real Canon color-related option (`CNColorSyncICC`, `CNColorHalftone`, `CNNumberOfColors`,
  `CNColorToUseWithBlack`, `CNXColorAdjustment`, `CNYColorAdjustment`) to confirm it stays narrow
  rather than grabbing an unrelated option via a loose substring match.
- **A brand-new queue cost two separate elevated prompts back to back** (queue-create, then a
  second `lpadmin` call for print defaults) - confirmed live these don't reliably share one cached
  authorization even ~15s apart. `PrintDefaultsForNewQueue` now reads the target PPD's own
  `*OpenUI`/`*CloseUI` declarations straight off disk (`readPPDFileOptions`/
  `parsePPDOpenUIOptions`, no live queue needed) so the `-o Key=Value` args can ride along on the
  *same* `lpadmin` call that creates the queue (`QueueOptions.ExtraOptionArgs`) - one prompt instead
  of two for any new queue. A reused existing queue (no queue-create call to piggyback on) still
  uses the old live-queue path (`SetPrintDefaults`), unchanged.

### Added
- **Install-phase timing** - a real Canon UFR II install was confirmed live to take 5m02s end to
  end with zero visible progress. `EnsureDriverInstalled` now runs `installer -verboseR` inside the
  same elevated call, capturing each phase transition with a real timestamp
  (`installVerboseTimestamped`) and logging an elapsed-seconds-per-phase breakdown
  (`logInstallPhases`) right after the install finishes - e.g. `Install phase "Running package
  scripts" took 288s.` `do shell script` can't relay progress *during* the wait, so this doesn't
  speed anything up by itself, but it turns "no visible progress" into real, first-party data on
  which phase actually owns the time - needed before targeting any further speed fix with evidence
  instead of guesswork.

### Testing
- `TestFindOption_MatchesCanonCNPrefixedDuplexBySuffix`,
  `TestFindOption_MatchesCanonCNColorModeBySuffix`,
  `TestFindOption_DoesNotMatchOtherRealCanonColorOptions`,
  `TestParsePPDOpenUIOptions_ParsesRealCanonDuplexBlock`,
  `TestParsePPDOpenUIOptions_ParsesRealCanonColorModeBlock`,
  `TestParsePPDOpenUIOptions_ParsesBothBlocksTogetherAndIgnoresSurroundingLines`,
  `TestDecidePrintDefaults_MatchesRealCanonPPDFileOptions`,
  `TestLogInstallPhases_ComputesElapsedSecondsPerPhaseIncludingTheLastOne`,
  `TestLogInstallPhases_IgnoresUnparsableAndPercentOnlyOutput`,
  `TestLogInstallPhases_EmptyOutputLogsNothing`.

## 2026-09-12 (v0.6.1) - macOS: fix real Deploy bugs found live (CUPS names, redundant installs); Subnet/JP polish

Found live testing 3 real Canon deploys end to end for the first time this session.

### Fixed
- **Deploy failed outright for any row named with a space** (`lpadmin: Printer name can only
  contain printable characters`) - a real, blocking bug, not a PDT quoting problem: `man lpadmin`
  confirms CUPS queue names can never contain SPACE, TAB, `/`, or `#` at all, unlike a Windows
  printer object name. `deploy_darwin.go`'s own `sanitizeCUPSQueueName` now replaces those four
  characters with `_` for the actual `-p` queue name only - `-D` (the queue's own description) and
  every other reference to `row.Name` (logging, `DeployResult`) keep the original, unsanitized
  name. Logs a `[WARN]` the one time a row's name actually needed changing.
- **The same driver package was reinstalled once per row, unconditionally** - confirmed live to be
  severe, not the "a few extra seconds" a stale doc comment assumed: installing a real Canon UFR II
  distribution package took **4m38s** end to end, so 3 identical test rows in one deploy cost ~14
  minutes of pure redundant work - and since each multi-minute install alone routinely outlasts
  macOS's own few-minutes authorization cache, back-to-back rows each triggered a fresh
  `osascript` password prompt too (the "multiple auth prompts to minimize to one" ask). Both
  `resolveDriver`'s guess-based fallback and `installVariant`'s model-index path now go through
  `Deployer.ensureInstalledOnce`, which skips a package this exact `*Deployer` (one per deploy run)
  has already installed and reuses the PPD list its first real install found - installing 3
  identical rows in one run now costs one real install, not three, and (in the common case of one
  manufacturer's packages all resolving early) needs at most one password prompt for the whole run
  rather than one per row.
- **The Driver field flashed the manufacturer's raw single-guess package label while typing a
  Model filter, before a real model was ever picked** - confirmed live: typing "5840" into Model
  doesn't fold-match any full model name yet, and the auto-fill was wired to `onChange` (fires on
  every keystroke), which called `DriverCandidates` - the function with the old guess-based
  fallback baked in - instead of stopping at "no match yet". `setupCombobox` (`frontend/src/
  main.js`) grew a proper `onCommit` callback (fired only from an actual dropdown pick or Enter,
  never plain typing); the Model combobox's Driver auto-fill moved there, so Driver only ever shows
  a real, model-derived value or stays blank/flagged - never a wrong guess mid-search.

### Added
- **Subnet field validation** (Defaults panel): flagged (the same yellow `input-needs-value` style
  used elsewhere) for anything that isn't 3 valid octets (0-255) with a single `.` between each,
  optional trailing `.` - catches an out-of-range octet, a double dot, a comma, or stray
  whitespace. Blank stays the plain white background (Subnet is optional, not mandatory) -
  `isValidSubnetPrefix` in `frontend/src/main.js`.
- **Japan-market-only Canon models are no longer indexed** - confirmed live against a real 641-
  model Canon catalog build that 166 of them (26%) end in " JP", and that this isn't just visual
  clutter: Canon's own raw PPD `*NickName` puts "JP" *after* the language token
  ("...C5840/5850 PS JP", not the reverse), so `stripLanguageSuffix`'s own trailing-token match
  never catches one at all - a JP variant never unified with its non-JP siblings the way the
  model-unification feature otherwise guarantees, on top of being useless for an English-only
  deployment. `isJapanMarketOnly` (`internal/driver/macmodel.go`) filters these out entirely
  (never indexed, not just hidden) at the same point in `indexFamilyPackage` for both the
  installer-backed and loose-PPD-bucket family shapes. A 9-model "EUR" suffix was also found in
  the same data and deliberately left alone - not obviously language-related, and not what was
  asked for; flagged for a decision later if it turns out to matter.

### Testing
- `internal/printer/darwin/deploy_darwin_test.go`: `TestSanitizeCUPSQueueName` (space/tab/`/`/`#`
  all replaced, everything else untouched, blank stays blank),
  `TestEnsureInstalledOnce_SkipsAlreadyCachedPackage` (a pre-cached package never reaches the real
  install path at all).
- `internal/driver/macmodel_test.go`: `TestIsJapanMarketOnly` (plain trailing " JP", the
  language-token-then-JP shape, and a same-suffix-but-not-actually-JP false-positive guard).

## 2026-09-12 (v0.6.0) - macOS: persistent per-manufacturer driver catalog; startup 35-45s -> ~2s

The startup/Refresh cost flagged the night before ("we need to fix that last part that causes the
launch to cost 35-45 seconds") - fixed two ways, timed and confirmed live against Ken's own real
Canon downloads and the actual PDT.app.

### Fixed
- **`pkgutil --expand-full` was the dominant cost, not PPD parsing** - timed against a real Canon
  UFR II package: 6.4s and 255MB written to disk, for a package whose actual PPDs total 25MB.
  `--expand-full` fully decompresses a package's *entire* payload (driver binaries, a dozen
  languages of README/license text, icons - everything) just so `packagePPDEntries`
  (`internal/driver/macppd.go`) could walk it looking for `*.ppd(.gz)`. Replaced with `pkgutil
  --expand` (structure only, ~0.1s) plus selective `cpio` extraction straight from each
  sub-package's own gzip-compressed Payload (`gunzip -c Payload | cpio -idm "*.ppd" "*.ppd.gz"` -
  confirmed live to be plain gzip'd cpio, no dependency on a more exotic format like pbzx) - 0.2-
  0.4s and 25MB. A ~20x cut on the actual bottleneck, using tools already in this codebase's own
  style (already shells out to `hdiutil`/`pkgutil`/`installer`).
- Parsing (549 real PPDs' `*NickName`) was already cheap (~2s total) and untouched - the walk
  itself was never the problem, what preceded it was.

### Added
- **`MacManufacturerCatalog`** (new `internal/driver/maccatalogdb.go`) - a persistent, versioned,
  human-readable JSON record of every model/PPD a family-preference manufacturer's model index has
  indexed, plus exactly which package (and its own parent chain - outer `.dmg` -> nested `.dmg` ->
  installer `.pkg` -> sub-package, each with path/modtime/size/version where one exists) produced
  it, and when. One file per manufacturer - `Drivers/macOS/<Manufacturer>/catalog.<manufacturer,
  lowercased>.json` (e.g. `Drivers/macOS/Canon/catalog.canon.json`) - not one combined file, so
  rebuilding or deleting one manufacturer's own catalog never touches any other's, ahead of this
  same system extending to other manufacturers' driver families later. Lives inside the Drivers
  folder itself, deliberately, so it travels with a portable/flash-drive copy - a technician's
  flash drive now carries its own already-built index from one Mac to the next.
- **`BuildMacModelIndex` skips re-inspecting (mounting, expanding, parsing) a package entirely**
  once its catalog entry matches - `MacManufacturerCatalog.IsCurrent` compares the outermost
  package's path/modtime/size (already free from `BuildMacCatalog`'s own directory scan, no
  mounting needed) against what's recorded, the same "a downloaded driver package is never
  silently modified in place, so a size match is as good as a hash" reasoning `copytree.go`'s own
  `listFileSizes` already uses. **Confirmed live: a second build against the same real Canon
  packages went from 17.7s to 0.147s** (~120x), with byte-identical results (641 models). The
  real app's own startup dropped from 35-45s to ~2s the same way.
- **`cachedVariantFilesExist`** guards the one real correctness gap this opened: catalog.json
  travels with the Drivers folder, but a no-installer family's cached PPD bytes
  (`~/Library/Application Support/PDT/PPDCache/` - deliberately *not* moved to live with the
  Drivers folder; a flash drive is normally write-protected in the field, and re-inspection is
  cheap enough now that there's no real benefit to it traveling too) don't. A fresh machine
  reading someone else's already-built catalog.json now re-inspects instead of trusting a
  `LooseCachedPPDPath` that doesn't resolve to anything locally yet.
- **Read-only on a removable drive** - `loadCatalog` (`app_darwin.go`) passes `persist=false` to
  `BuildMacModelIndex` whenever this exact running copy is on a removable drive
  (`flashdrive.IsRemovableDrive`, already used for the same reason on the Windows side's own
  `BuildCatalogNoExtract`): a technician's laptop is where catalog.json gets created/updated
  (already fast to rebuild there, on local NVMe); a flash drive plugged into a different machine
  only ever reads whatever's already there, never writes back to it.
- **Version-diff surfaced, not just tracked** - `DiffModels` compares a family's newly-indexed
  model names against what the catalog had recorded before a package changed, and
  `CatalogStatus.ModelChanges` carries a human-readable summary (`"Canon UFR II: +3 new: ..., -1
  no longer present: ..."`) back to the frontend's Log panel after a Refresh that actually found
  something different - the raw "what changed between this package and the last one" material
  asked for, without PDT itself trying to judge staleness or regressions (a human call once they
  can see what actually changed). Empty on a first-ever build (nothing to diff against yet).
- **`cmd/pdtdebugmac models <driversRoot>`** now also reports build time and any model-diff
  output - this is what surfaced both the `--expand-full` bottleneck and confirmed the cache-hit
  path actually works, against real files, before touching the real app.

### Testing
- `internal/driver/maccatalogdb_test.go` (new): filename convention, load/save round-trip
  (including the full provenance chain), `IsCurrent` (path/modtime/size match and mismatch),
  `DiffModels`, diff-summary truncation.
- `internal/driver/macmodel_test.go`:
  `TestBuildMacModelIndex_SecondBuildReusesCatalogWithoutReinspecting` - a second build against
  the same packages is near-instant and produces an identical index; existing tests updated for
  the new signature, all still passing (the fast-extraction rewrite preserves exact behavior).

## 2026-09-12 - Canon Model/Driver: no more raw package-name defaults; auto-derive Driver from Model on macOS

### Fixed
- **`DefaultDriverFor` on macOS still defaulted the Defaults panel's Driver field to a raw package
  label** (`"UFRII_v10.19.25_mac"` - the driver *package's* own filename, not a real driver name)
  for any manufacturer with real per-model data (Canon), even after fixing `MacModelCandidates`'
  own blank-model bug earlier today - `DefaultDriverFor` is a separate function and wasn't touched
  by that fix. It now returns `""` for those manufacturers instead (`drivercatalog_darwin.go`) -
  correct, since the Defaults panel has no Model field to narrow by at all, so there was never a
  single genuinely-correct package to guess among Canon's UFR II/PostScript/Generic PPD. Every
  other manufacturer (one real package, no ambiguity) is unaffected.

### Added
- **New `App.MacModelManufacturers()`** (macOS-only bound method) lists every manufacturer with
  real per-model PPD data (Canon today), letting the frontend ask "does this manufacturer need
  Model to pick a driver" without hardcoding a manufacturer name or exposing
  `macFamilyPreference` directly. Backs three behavior changes, all conditioned on it
  (`macModelDriven()` in `frontend/src/main.js`), platform-specific per Ken's own spec:
  - **Windows, Manufacturer = Canon**: unaffected/confirmed-already-working - `DefaultDriverFor`
    already resolves to "Canon Generic Plus UFR II" via the existing `defaultDriverTokens`
    (`internal/driver/default.go`, `{"UFR", "II"}` - already ships from before this session). A
    grid row's own Driver now auto-fills on Manufacturer change too (previously cleared to blank)
    - the Defaults panel's own current Driver value when its Manufacturer already matches
    (preserving a manual override the technician typed there), otherwise a fresh
    `DefaultDriverFor` call for the newly-picked manufacturer - mirroring how `addPrinterRow`
    already sources a *new* row's Driver. Model is unaffected: already optional, blank, typable,
    and fuzzy-searched when data exists (Kyocera) - genuinely infeasible to extend to Canon's own
    mac model data from Windows, since indexing it depends on `hdiutil`/`pkgutil` (macOS-only).
  - **macOS, Manufacturer = Canon, Defaults panel**: Driver field is blank and no longer flagged
    as needing a value (`DefaultDriverFor`'s own fix above, plus the needs-value toggle at every
    call site now checking `macModelDriven`).
  - **macOS, Manufacturer = Canon, grid row**: Model is now mandatory (flagged when blank) for a
    macModelDriven manufacturer, and committing a Model (typed or picked) auto-fills Driver to
    that model's own preferred variant - `DriverCandidates(manufacturer, model, "")`'s first
    result, which is already preference-ordered UFR II-first (`MacModelCandidates`'s own doc
    comment) - so this is exactly "the respective model-specific UFR II variant", with no new
    Go-side lookup needed. Clears back to blank/needs-value the moment the typed Model text no
    longer resolves to a real one, so Driver never silently keeps pointing at stale data.

### Testing
- `drivercatalog_darwin_test.go` (new): `TestDefaultDriverFor_BlankForManufacturerWithModelIndex`,
  `TestDefaultDriverFor_NoModelIndexEntryUnaffected`,
  `TestMacModelManufacturers_SortedKeysOfModelIndex`.

## 2026-09-12 - Fix Canon Model/Driver dropdown collapsing to one raw label with Model blank

### Fixed
- **A real, previously-deferred bug, root-caused against Ken's own real Canon downloads**: with
  Canon selected and Model left blank (the default state), the Driver dropdown showed exactly one
  entry - a raw, unparsed label like `"UFRII_v10.19.25_mac"` - instead of the hundreds of real,
  friendly model names the catalog actually has. Root cause: `MacModelCandidates`
  (`internal/driver/macmodel.go`) calls `lookupMacModel` first, which treats a blank model as
  "nothing to look up" and returns not-found - so `MacModelCandidates` returned `[]string{}` for
  *any* blank Model, regardless of whether the index had data, and `App.DriverCandidates`
  (`drivercatalog_darwin.go`) silently fell through to its `ResolveMac` fallback, which only ever
  offers the single package it guessed was newest. Confirmed via a new `pdtdebugmac models`
  command (see below) that `BuildMacModelIndex` itself was working perfectly all along - it
  indexed all 641 of Ken's real Canon models correctly; the bug was entirely in how a blank Model
  was handled one layer up.
- **Fix**: `MacModelCandidates` now has its own blank-model branch - lists every variant of every
  model the index has for the manufacturer (sorted alphabetically by model, then by
  `macFamilyPreference`'s own language order within each), the same "un-narrowed, list everything"
  behavior Windows' own `Candidates` (`internal/driver/candidates.go`) already has for a blank
  model - `DriverCandidates`'s own doc comment claims to mirror that two-step Model-narrows-Driver
  behavior, which requires the blank case to match too. `lookupMacModel` itself is untouched -
  `MacVariantForDeploy`'s exact-match deploy-time lookup still correctly treats a blank model as
  not-found there.
- **`cmd/pdtdebugmac models <driversRoot>`** (new debug subcommand): builds the real
  `BuildMacModelIndex` against real files and prints every manufacturer/model/variant it finds,
  plus what `App.Models`/`App.DriverCandidates` would actually return - this is what surfaced the
  bug precisely (full model index correct, `MacModelCandidates` alone empty for blank Model).
- Also found and cleaned up in the same session: a stale, never-detached `.dmg` mount
  (`/Volumes/CANON_MAC`) left over from an earlier catalog build - not the cause of this bug, but
  a real resource leak worth knowing about if mounted-volume clutter ever comes up again.

### Testing
- `internal/driver/macmodel_test.go`: `TestMacModelCandidates_BlankModelListsEveryModel` - a blank
  model returns every model's variants (both test fixture models), grouped by language then
  sorted by model name, not the empty list this bug produced.

## 2026-09-12 - Log an entry when Refresh Drivers actually starts, not just when it finishes

### Fixed
- **Clicking the toolbar's Refresh button gave no feedback that anything had happened** - a real
  driver folder scan can take 30-45+ seconds (extracting/inspecting archives), and the only
  existing signal was the icon button going disabled, confirmed live to read as "did the click
  even register?" rather than "working on it," for the entire span until the eventual OK/ERR log
  line. Added a `logStatus('INFO', 'Refreshing driver catalog...')` right before the
  `RefreshDriverCatalog` call (`frontend/src/main.js`), so the Log panel shows a start line
  immediately instead of going quiet until the (identically-worded-either-way) finish line
  eventually appears.

## 2026-09-12 - Fix LPD-Q not tracking a later Manufacturer change; grid header shortened

### Fixed
- **LPD-Q's manufacturer default only applied once, live-tested and confirmed wrong**:
  switching a grid row from Canon to HP correctly set LPD-Q to `raw`, but switching that same
  row on to Xerox (or anything else) left the stale `raw` behind instead of updating to `lp` (or
  blank) - the previous "only fill when blank" guard meant the default fired exactly once per
  row and never again. `defaultLpdQueueFor` is now applied unconditionally every time a row's
  Manufacturer changes (`frontend/src/main.js`), so LPD-Q always reflects whichever manufacturer
  is currently selected. Still just a default, not enforced: typing a custom value afterward is
  untouched by anything except picking a (possibly the same) manufacturer again.

### Changed
- Grid column header "1-sided" shortened to "1-side" to save space - grid-only; the Defaults
  panel's own checkbox label and the CSV column name (`1-sided`, unchanged for compatibility with
  already-saved CSV templates) are untouched.

## 2026-09-12 - Add per-row LPD-Q override (Windows + macOS), closing the last known-issue item

### Added
- **New "LPD-Q" grid column, right after IP** (`frontend/src/main.js`) - a free-text, genuinely
  optional per-row override for the queue-name segment of a macOS deploy's LPD device URI
  (`lpd://<ip>/<LPDQueueName>` - `internal/printer/darwin/deploy_darwin.go`'s own `Deploy` always
  used a bare `lpd://<ip>/`, no override, until now - the last item on the v0.4.0 known-issues
  list). Never gets the yellow `input-needs-value` styling every other required grid field does -
  most manufacturers' MFDs ignore the LPD queue name entirely and respond to any/no value.
- **Auto-filled for the two manufacturers confirmed to actually need one** - HP ("raw") and Xerox
  ("lp") - the moment a row's Manufacturer becomes either one (`defaultLpdQueueFor`, applied both
  when "Add Printer" creates a row from the Defaults panel and when an existing row's own
  Manufacturer dropdown changes). Only fills a currently-blank value - never overwrites a value
  already typed by hand, whether that came from a prior default or a deliberate override.
- **`printer.PrinterRow.LPDQueueName` (new field, both platforms) round-trips through Open/Save
  Configuration (`config.SavedRow`) and CSV (`New CSV`/`Import CSV`) on Windows too, even though
  Windows' own `Deploy` never reads it** (Standard TCP/IP ports have no LPD-queue concept at all)
  - deliberate, ahead of using the same saved configuration to deploy the same printers from
  either platform: a config saved on Windows already carries the right value the moment it's
  opened on a Mac, with no per-row redo needed. An older CSV/config saved before this field
  existed still loads fine with it simply blank (`ImportCSV`'s column lookup was already
  by-name and tolerant of a missing one).
- Device URI construction pulled out into a small pure `lpdDeviceURI(ip, queueName)` (trims stray
  slashes/whitespace off a hand-typed value like `"/raw"` or `"raw/"`), so it's directly unit-
  testable without needing to mock the rest of `Deploy`'s real CUPS calls.

### Testing
- `internal/printer/darwin/deploy_darwin_test.go`: `TestLpdDeviceURI` - blank/HP/Xerox defaults,
  plus stray-slash/whitespace trimming.
- `internal/config/json_test.go`/`csv_test.go`: `LPDQueueName` round-trips through both Open/Save
  Configuration and CSV import, and an old CSV missing the column still imports with it blank.

## 2026-09-12 - macOS: fix the Defaults-panel spacing bug (WebKit legend-in-flex quirk)

### Fixed
- **The "minor Defaults-panel spacing/padding issue above the Manufacturer row" noted (but not
  reproduced) since v0.4.0** - reproduced via a real screenshot, and root-caused: `.defaults-outer`
  (`frontend/src/app.css`) was a `<fieldset>` with `display: flex; flex-direction: column` applied
  directly to it, with `<legend>` as one of its flex-context children alongside `.defaults-row` and
  the Port/Print Defaults/Advanced sub-fieldsets. WebKit (the macOS Wails webview) renders that
  `<legend>` as a genuine flex item in that situation - consuming its own row height *plus* one full
  flex `gap` beneath it - while Chromium (Windows' WebView2) correctly excludes the legend from flex
  layout entirely, per the fieldset/legend rendering spec. Identical CSS, visibly more empty space
  above the Manufacturer row on macOS only. The Port/Print Defaults/Advanced sub-fieldsets never
  showed this because they're row-direction flex (the bare `fieldset` rule's own default), where an
  extra legend-as-flex-item only costs horizontal space, not vertical - only `.defaults-outer` used
  column direction.
- **Fix**: moved the flex/gap layout off the `<fieldset>` itself and onto a new plain `<div
  class="defaults-body">` wrapping `.defaults-row` and the three sub-fieldsets (`frontend/src/
  main.js`) - `.defaults-outer` is back to a plain block-level fieldset (native, unambiguous legend
  rendering on any engine), and the original padding values (`4px 12px 8px`) are preserved exactly,
  just relocated across the fieldset/wrapper split so the same visual rhythm applies once the
  flex-legend ambiguity is gone.
- **Second-order bug found immediately after, via a live screenshot**: `.defaults-row`'s own
  `margin-bottom: -18px` (an earlier eyeballed hack pulling the row closer to the Port fieldset
  below it, to compensate for the row having no border of its own) no longer just looked "slightly
  tight" once the fix above removed the WebKit legend-height bug it had unknowingly been tuned
  against - it now visibly overlapped the Port box's own legend/border. Removed entirely; the row
  now uses the same plain 8px `.defaults-body` gap as every other boundary in the panel - a real,
  positive number that can't turn into an overlap on either engine, at the cost of a hair more
  (harmless) whitespace than the original hand-tuned value gave.

## 2026-09-11 - macOS: fix Browse-dialog focus theft; scaffold the Drivers tree; tooltip fix

Follow-up from live-testing the previous entry's `osascript` folder-picker fix on real hardware.

### Fixed
- **The folder-picker's own "choose folder" dialog was bringing Finder to the foreground instead
  of PDT** (`pickfolder_darwin.go`) - confirmed live: the first click made Settings appear to
  vanish entirely (PDT's own window just went behind Finder, since Settings is HTML inside that
  same window). Root cause: `choose folder` run bare through `osascript`, with no `tell
  application` wrapper, belongs to no particular app - macOS attributes its window to Finder and
  activates it as a side effect. Fixed with the standard AppleScript idiom for this: capture
  `path to frontmost application` before running `choose folder`, and explicitly reactivate it
  afterward on both the success and Cancel paths (re-raising the original error/exit code on
  Cancel so the existing `-128` detection in Go is unaffected).
- **The toolbar's Drivers-folder icon's tooltip said "Open the Drivers folder in File
  Explorer"** on every platform, including macOS, which has no File Explorer - it has Finder.
  Reworded to "Open Drivers folder" (`frontend/src/main.js`), OS-agnostic rather than naming
  either platform's file manager.

### Added
- **`ensureMacDriversScaffold` (`driversfolder.go`), wired into `app_darwin.go`'s
  `platformStartup`** - the darwin analog of the Windows side's own `ensureDriversScaffold`,
  closing the "no macOS Drivers-folder scaffold yet" gap noted since the v0.4.0 macOS port. Not a
  straight port - macOS driver packages genuinely vary by OS release the way Windows' generally
  don't, so `Drivers/macOS/<Manufacturer>/<macOS version>/...` nests the *opposite* way from
  Windows' `Drivers/Windows/<version>/<Manufacturer>/...` (version under manufacturer, not
  manufacturer under version). Unconditional/idempotent like the Windows version, but two
  different things: (1) ensures every `driver.Manufacturers` entry has at least a bare
  `Drivers/macOS/<Manufacturer>` folder to pick from, even before any macOS package exists
  locally; (2) retroactively drops an `Archive/README.txt` into every macOS-version subfolder it
  finds already there under each manufacturer - confirmed necessary against a real Drivers
  folder (Ken's own): only 2 of Canon's 10 real version folders had an `Archive` folder at all,
  and none of them had a `README.txt`, since nothing had been creating this automatically until
  now. Deliberately does **not** create any version subfolder itself, unlike the Windows side's
  own hardcoded `Windows/11` - there's no one macOS version this could hardcode that wouldn't go
  stale the moment Apple ships the next one (macOS 26 "Tahoe" -> 27 "Golden Gate" needed exactly
  that, by hand, the same day this was written - see below).

### Testing
- `driversfolder_test.go`: `TestEnsureMacDriversScaffold` - every manufacturer gets a bare
  folder, every existing version folder gets `Archive/README.txt` backfilled, and a manufacturer
  with no version folder at all stays empty rather than getting one invented.

### Housekeeping
- Added `27-GoldenGate/Archive` under Canon/HP/Kyocera/Ricoh/Sharp in the real Drivers folder
  (`~/Library/Application Support/PDT/Drivers/macOS/`), alongside each manufacturer's existing
  `26-Tahoe` - preparing for macOS 27 ("Golden Gate") ahead of any driver packages actually being
  available for it yet.

## 2026-09-11 - macOS: fix Settings > General's "..." Browse buttons never opening a dialog

### Fixed
- **Settings > General's three Browse (`...`) buttons - Save File/Drivers/Preinstall Base
  Path - now actually open a folder picker on macOS.** Root-caused: every Wails-bound Go
  method, `PickFolder` (`app.go`) included, runs on its own freshly spawned goroutine, never
  the process's real main thread (confirmed in Wails' own vendored darwin frontend code -
  each JS-to-Go call is dispatched via a bare `go func(){...}()`). AppKit's `NSOpenPanel` is
  only documented-safe to drive from the main thread; calling its
  `beginSheetModalForWindow:completionHandler:` off-thread is what silently swallowed every
  click - the sheet never actually appeared, and Wails' own darwin `dialog.go` surfaced no
  error either, since it just blocks forever reading the response channel the sheet's
  never-fired completion handler would have sent on (matches the "Browse buttons don't open a
  folder picker on macOS - not yet root-caused" line in the v0.4.0 known-issues list). Wails v2
  is end-of-life - there's no newer release to pick up a fix from, and correctly patching its
  own Objective-C would mean splitting "present the sheet" from "wait for the result" so the
  main thread's run loop stays free to actually deliver that result; blocking the main thread
  for the whole wait (the naive fix) would just trade one hang for a guaranteed one.
- **`PickFolder`'s actual dialog now goes through a new per-platform `pickFolderDialog` hook**
  (`pickfolder_windows.go` keeps calling Wails' own `runtime.OpenDirectoryDialog` exactly as
  before; `pickfolder_darwin.go` shows the dialog via AppleScript's `choose folder` through
  `osascript` instead) - `osascript` runs entirely in its own process, with its own main
  thread and run loop, sidestepping the whole problem rather than needing to fix Wails' own
  code. Confirmed live: the AppleScript dialog reliably appears where the old call never did.
  `POSIX path of (choose folder ...)` always returns a directory path with a trailing `/`
  (AppleScript's own convention) - `filepath.Clean`ed so Settings displays/stores the same
  shape on either platform.

### Testing
- `pickfolder_darwin_test.go`: `appleScriptString`'s quoting/escaping (embedded `"` and `\`) -
  the only pure-Go part of the new path that's meaningfully unit-testable without popping a
  real dialog.

## 2026-09-10 - macOS: build-once model->PPD catalog for Canon (replaces per-deploy guessing)

### Added
- **`driver.MacModelIndex` (`internal/driver/macmodel.go`)**: a real Kyocera-on-Windows-style
  model catalog for macOS - `manufacturer -> friendly model -> []MacPPDVariant` - built once
  (`BuildMacModelIndex`) at the same points `BuildMacCatalog` already runs (startup, Refresh
  Drivers), for every manufacturer `macFamilyPreference` lists (Canon today). Inspects only each
  family's own single newest package, not every version-folder's own copy, keeping the one-time
  cost bounded. `App.Models`/`App.DriverCandidates` (`drivercatalog_darwin.go`) are now real,
  catalog-backed lookups on macOS - Model narrows Driver's own candidate list exactly the way it
  already does for Kyocera on Windows, offering language-variant labels like
  `"Canon iR-ADV C5840/5850 (UFR II)"` / `"(PostScript)"` / `"(Generic PPD)"`.
- **`deploy_darwin.go`'s `resolveDriver` now resolves a row's `(Model, Driver)` straight to a
  `driver.MacPPDVariant` (`driver.MacVariantForDeploy`) with no package re-inspection at deploy
  time at all** - a real behavior change from the v0.5.4 design, which called
  `pkgutil --expand-full` fresh on every single deploy just to guess which package/PPD applied.
  The catalog already knows which package to install and which exact PPD filename it registers
  (or, for the no-installer family below, a permanently-cached PPD to use directly), pulled from
  a build-once index instead of live per-deploy inspection.
- **`driver.LocateLoosePPDs`/`driver.CachePPDFile`**: a real finding while building the catalog -
  Canon's own "PPD" bucket download (`PPDv5.50_mac.dmg`) turned out to have no `.pkg` installer
  inside it at all, just a nested `.dmg` wrapping a plain folder-per-model tree of loose
  `*.PPD.gz` files. Confirmed by reading one directly: no `*cupsFilter` line, a real Generic
  PostScript Level 3 PPD - unlike UFR II/PS, whose PPDs declare `*cupsFilter` entries pointing at
  vendor filter binaries only `installer -pkg` deposits, this family needs no install step at
  all. `LocateLoosePPDs` mounts/locates these the same way `LocatePkg` does (one level of nested
  `.dmg`), and `BuildMacModelIndex` permanently copies each matched PPD to
  `~/Library/Application Support/PDT/PPDCache/` (the source `.dmg` won't still be mounted at
  deploy time) - deploy hands that cached path straight to `lpadmin -P`, skipping `installer`
  entirely for this one family.
- **`stripLanguageSuffix`**: confirmed against all three of Canon's real downloads for the same
  physical model that each family's own PPD `*NickName` differs only by a trailing language
  token - `"Canon iR-ADV C5840/5850"` (UFR II, no suffix) vs. `"...PS"` vs. `"...PPD"` - so
  stripping that known token is what unifies all three into one canonical model name for the
  Model dropdown.

### Changed
- The v0.5.4 guess-based path (`driver.ResolveMacFamily` + `choosePPD`) still exists and still
  runs, now strictly as the fallback for whatever the catalog doesn't have an entry for: a
  manufacturer `macFamilyPreference` doesn't list, Model left blank, or a typo that doesn't
  fold-match any known model. Every manufacturer with just one real driver package (Kyocera,
  Ricoh, Sharp - no family table, so no catalog entry at all) is unaffected and keeps using this
  path exactly as it did in v0.5.4.

### Testing
- `internal/driver/macmodel_test.go`: 11 tests against `internal/driver/testdata_mac_model/` -
  two real `pkgbuild`-built `.pkg` fixtures (UFR II, PostScript) plus one real nested-`.dmg`
  fixture built with `hdiutil` (no `.pkg` inside, matching the real "PPD" bucket shape), covering
  cross-family model unification, a model with only one language variant, ranking/filtering,
  explicit-label-wins-over-guess deploy resolution, case/space-insensitive model matching, and
  the "no catalog entry" fallback signal.
- All validated first against the user's real Canon UFR II/PS/PPD downloads (all three families'
  real `*NickName`/`*cupsFilter` content) before being reduced to permanent, fast synthetic
  fixtures.

## 2026-09-10 - macOS: model-driven driver-family and PPD selection (Canon UFR II/PS/PPD)

### Added
- **`driver.ResolveMacFamily` (`internal/driver/macfamily.go`)**: for a manufacturer that ships more
  than one genuinely distinct driver as separate packages - not just version variants of the same one,
  which `ResolveMac`'s plain newest-by-mtime pick already handled - resolves the row's Model against
  each family's own PPD payload (newest-first, in a declared preference order) before picking which
  package to install. `macFamilyPreference` today has one entry, matching Ken's own stated preference:
  `"Canon": {"UFRII", "PS", "PPD"}` (UFR II first, then PostScript, then the plain-PPD-only package).
  Falls back through lower-preference families with a `[WARN]` note if the preferred family's PPDs
  don't cover the requested model, and all the way back to plain `ResolveMac` (also warned) if none of
  them do, or Model is blank. Every other manufacturer (no family table) is unaffected.
- **`internal/driver/macppd.go`**: `ReadPPDNickName` reads a PPD's own `*NickName`/`*ModelName` field
  (transparently gzip-decompressing `.ppd.gz`); `PackagePPDNickNames`/`PackageBestModelScore` do the
  same read-only `pkgutil --expand-full` inspection `PackageLabel` already used, but collect every
  PPD's NickName and score it against a model string - lets a package be checked against Model
  *before* installing it, which is what `ResolveMacFamily` uses to compare families.

### Fixed
- **`choosePPD` (`deploy_darwin.go`) was matching a newly-installed PPD's *filename* against Model,
  and Canon's real filenames make that a hard failure.** Confirmed live against a real Canon UFR II
  package (mounted the actual `.dmg`, which contained a nested `.dmg`, containing
  `UFRII_LT_LIPS_LX_Installer.pkg`) that the PPD for the iR-ADV C5840 is filed as
  `CNPZUIRAC5840ZU.ppd.gz` - a cryptic vendor code sharing no matchable substring, or even in-order
  character sequence, with how a technician would type the model (`iR-ADV C5840`);
  `FuzzyMatchScore`'s subsequence fallback specifically fails on the `-` and ` ` characters the
  filename never contains, so this was a hard `-1`, not just a weak match. That PPD's own `*NickName`
  reads `"Canon iR-ADV C5840/5850"` - `choosePPD` now reads each candidate's NickName
  (`driver.ReadPPDNickName`) before scoring, falling back to the filename only when a PPD has none at
  all. Fixes model selection for every manufacturer, not just Canon.

### Testing
- `internal/driver/macfamily_test.go`: 6 tests against synthetic `pkgbuild`-built fixtures
  (`internal/driver/testdata_mac_family/`, standing in for Canon's real UFRII/PS/PPD split) covering
  the direct match, each fallback tier, the no-match-anywhere case, a blank Model, and a manufacturer
  with no family table.
- `internal/printer/darwin/deploy_darwin_test.go`: new `TestChoosePPD_MatchesRealCanonStyleCrypticFilenameByNickNameContent`
  proves the actual bug this was built to fix, using real cryptic Canon-style filenames with real
  NickName content.
- All validated first against the user's real Canon UFR II download before being reduced to permanent,
  fast synthetic fixtures.

## 2026-09-10 - macOS: auto-extract .zip driver packages (Canon ships this way)

### Fixed
- **Canon's real macOS driver downloads (UFR II, PS, PPD - all three) ship as a `.zip` directly
  wrapping one `.dmg`, which `BuildMacCatalog` didn't recognize at all** - confirmed live against a
  real Drivers folder (`internal/driver/maccatalog.go`'s `macPackageExts` only ever matched `.dmg`/
  `.pkg`), so Canon silently showed as having nothing locally despite real files being present. New
  `internal/driver/maczip.go`'s `ensureMacZipsExtracted` runs before the catalog walk (same "extract
  first, then let the generic scan find whatever's inside" order `BuildCatalog`'s own
  `ensureZipsExtracted` already uses on the Windows side) - reuses that same file's `extractZip`/
  `flattenRedundantWrapperDir` directly, since neither has any Windows-only dependency at all.
- **A second bug found while fixing the first one**: macOS's own zip tooling (Archive Utility, or
  anything else zipping a folder on a Mac) litters `__MACOSX/._<name>` AppleDouble resource-fork stubs
  into the archive - confirmed live one of these shares the real file's own `.dmg` extension, at a few
  hundred bytes instead of 80+ MB, so without explicitly skipping `__MACOSX/` directories and `._`-
  prefixed files, it showed up as a second, bogus catalog entry that would fail the moment something
  tried to mount it (`LocatePkg`/`mountDmg`). Applied to both the installer-package scan and the
  OpenPrinting PPD bucket scan.

## 2026-09-10 - Unified the portable-copy default path literal across platforms

### Changed
- **Settings > General's portable-copy Drivers/Configs base path no longer shows a platform-specific
  separator on either OS.** The v0.5.1 fix below split the portable-copy default into
  `settings_windows.go` (`.\Drivers`/`.\Configs`) vs. `settings_darwin.go` (bare `Drivers`/`Configs`) -
  Ken's own follow-up question ("when running from flash, it could be from a mac or Windows... needs
  to change dynamically") led to a simpler answer: since a bare relative name means exactly the same
  thing to `resolveExeRelative` on every platform, there was never a real need for either side to echo
  its own OS's separator convention at all. Windows' portable case now also returns bare
  `Drivers`/`Configs` (no leading `.\`), matching macOS exactly - the displayed base path can no longer
  look "wrong" (or actually resolve wrong) if the same flash drive/technician workflow ever moves
  between a Windows and a macOS machine. Also fixed two frontend tooltips (`saveFileBasePath`/
  `driversBasePath` in `frontend/src/main.js`) that still hardcoded `%LocalAppData%\PDT\...` and a
  trailing `\` - Windows-only text shown to macOS users too, since the tooltip strings are shared.

## 2026-09-10 - macOS: Model-driven PPD selection; Windows-style paths bug fixed
## 2026-09-10 - macOS: Model-driven PPD selection; Windows-style paths bug fixed

### Added
- **Model-driven PPD selection on macOS** - the planned work the Windows-side v0.5.0 session (Model
  field back in the grid) explicitly deferred for a real macOS session to pick up. `deploy_darwin.go`'s
  `resolveDriver` now uses the row's own Model to pick the right PPD both when an installer package
  registers several (`choosePPD`, fuzzy-matched, `[WARN]`s rather than silently guessing when the match
  isn't confident - see `ambiguous`'s own doc comment for exactly what counts as confident) and in the
  OpenPrinting-bucket fallback (`row.Driver`'s exact dropdown selection preferred over a fresh Model
  fuzzy-match when there is one - `driver.OpenPrintingPPDByLabel`). `App.DriverCandidates` on darwin
  also now actually uses `model` (previously accepted but ignored) to narrow the Driver dropdown's own
  candidates, matching Windows' own Candidates behavior. See the README's "Model-driven PPD selection
  on macOS" section for the full writeup, including what's still genuinely unbuilt (an interactive
  disambiguation prompt for a real tie, versus today's log-a-warning-and-guess).

### Fixed
- **A real, until-now-shipped bug**: `settings.go`'s default Drivers/Configs base paths were hardcoded
  to Windows path syntax (`.\Drivers`, `.\Configs`, `%LocalAppData%\PDT`) with no macOS equivalent -
  confirmed live as more than a cosmetic display issue (the "Known issues" list in the v0.4.0 entry
  below undersold it): handing `.\Drivers` to `filepath.Join`/`resolveExeRelative` on macOS doesn't
  split on the backslash at all, so it created a folder literally *named* `.\Drivers` right next to the
  running `.app`'s own executable, severely enough that it broke `wails build`'s own codesign step once
  that malformed folder existed inside the bundle (`codesign failed... bundle format unrecognized`).
  Split into `settings_windows.go`/`settings_darwin.go`; the macOS side resolves a portable copy's
  relative path as plain `Drivers`/`Configs` (no backslash to begin with) and an installed copy's base
  to `~/Library/Application Support/PDT` (the macOS analog of `%LocalAppData%\PDT`). This may also
  explain (not yet confirmed) the previously-reported Settings > General browse-button issue - a
  malformed starting directory fed to the native folder picker is a plausible cause - worth re-testing
  now.

## 2026-09-09 (v0.4.0) - macOS support (data layer, darwin Deployer, Wails app, frontend)
## 2026-09-10 - Added: Model field back in the grid; planned macOS PPD-by-model work noted

Ken's idea: Windows-side row data is already nearly enough to create macOS CUPS LPD queues too - the
main gap is that macOS PPDs are often model-specific, and PDT had no per-row way to record a model at
all (`PrinterRow.Model`/`SavedRow.Model`/CSV's own `Model` column all already existed end to end in the
data layer - only the interactive grid itself never had an input for it). Theoretical/future macOS work
noted below rather than built now, since Ken is on Windows and it needs live iteration against real
macOS PPD data to get right.

### Added
- **Model field back in the grid** (`row-model`, between Manufacturer and Driver) - optional free text,
  never highlighted as needing a value (unlike Name/IP/Manufacturer/Driver). Narrows the Driver
  dropdown's own candidate list the same way typing a model straight into Driver's filter text already
  did (`driver.Candidates` already accepted a `model` argument - this just finally feeds it a real
  value instead of always `""`). A suggestions dropdown only appears when real per-model data actually
  exists for the selected manufacturer - Kyocera only, today, via the existing model index
  (`driver.BuildModelIndex`) - anyone else's Model field is honestly just a plain text box, no fake
  search offered. New `driver.Models(modelIndex, manufacturer, filterText)` (ranked by
  `FuzzyMatchScore`, mirroring `Candidates`), replacing the never-actually-called-from-the-frontend
  `App.Models(manufacturer)` that already existed. Selecting a different Manufacturer clears Model, same
  as it already clears Driver.
- **Planned (not yet implemented): use Model to pick a model-specific PPD during macOS Deploy.**
  `driver.ResolveOpenPrintingPPD(catalog, manufacturer, model)` already does the actual fuzzy-matching
  logic this needs - it's just never called from the real deploy path today (only
  `OpenPrintingCandidates`, which drives the interactive Driver dropdown, is). Still unbuilt: prompting
  the technician with candidate PPDs (or the full OpenPrinting bucket for that manufacturer) when
  narrowing by model is ambiguous or fails, instead of silently guessing wrong. See the README's own
  "Planned: Model-driven PPD selection on macOS" section (under "macOS support") for the fuller writeup
  to pick this back up from on a real macOS session.

## 2026-09-09 - Fixed: flash copy ETA swinging wildly (v0.4.2 follow-up)

Ken reported the new ETA (v0.4.2) swinging from 150-160 minutes down to 37, then up to 44, while
copying a real Drivers folder - exactly matching a known failure mode (the same one Windows' own copy
dialog is notorious for). Root cause: the ETA averaged bytes-done over the *entire* step's elapsed
time. A real Drivers folder's file sizes are bimodal - long runs of tiny files (.cat/.inf) where
per-file open/write/close overhead dominates almost independent of actual byte count, interrupted by a
handful of huge installers - so a slow, overhead-bound run of small files drags that average down hard,
and it stays wrong for a long time afterward even once a big file's real throughput starts coming in,
because the average has to "unwind" every sample since the step began before it reflects current
conditions at all.

Fixed by replacing the since-the-start average with a time-decayed windowed rate (`etaEstimator`,
`flashdrive.go`): each new sample decays a running (bytes, seconds) pair by
`e^(-realElapsed/etaRateTimeConstant)` (6 seconds) before adding its own delta - decaying by actual
wall-clock time elapsed, not by call count, so a burst of a thousand tiny files arriving within
milliseconds barely decays the window at all, rather than being treated as if a lot of real time had
passed. This means the estimate "forgets" a slow phase within roughly the time constant's own span once
real throughput changes, instead of staying anchored to minutes of stale history. Added
`TestEtaEstimator_RecoversQuicklyAfterRateChanges` as a direct regression test, simulating a full
minute of slow throughput followed by a rate increase and confirming the estimate converges toward the
new rate rather than staying dragged down by the old one.

## 2026-09-09 - Improved: faster flash drive copy, with a time estimate

Ken reported a manual File Explorer copy of the real Drivers repo took about 21 minutes to a freshly
formatted flash drive, and PDT's own Write to Flash Drive gave no sense of how much longer it had left
- especially misleading with a file-count-based percentage once file sizes vary as wildly as a real
Drivers folder's do (thousands of tiny files, then one huge installer).

### Changed
- **`copyTreeMerge` (`copytree.go`) now copies files on a small worker pool** (`copyTreeWorkers = 4`)
  instead of one file at a time - a newly formatted destination gets zero benefit from the existing
  skip-unchanged-file optimization (there's nothing to skip yet), so every file has to actually be
  copied, and USB media is latency-bound as much as bandwidth-bound: overlapping a handful of files'
  worth of per-file open/write/close latency helps more than raw sequential throughput alone. Directory
  creation stays a single sequential pass first (cheap, and sidesteps any concurrent-`MkdirAll`
  question entirely) before file copies are handed to the pool.
- **`copyFile` now copies with an explicit 1MB buffer** (`io.CopyBuffer`) instead of `io.Copy`'s default
  32KB, cutting the read/write syscall count for the larger driver installer files a real Drivers
  folder also contains.
- **The copy-progress dialog now shows a time estimate** ("~Xm Ys remaining") and its bar is now
  byte-based, not file-count-based, for the same file-size-variance reason above - `CopyProgress` now
  carries byte totals alongside file counts, and `newFlashCopyProgressFunc` (`flashdrive.go`) estimates
  time remaining from the current step's own observed bytes-per-second, waiting at least 2 seconds of
  real throughput data before showing anything rather than flashing an unstable estimate from the very
  first file.

### Fixed
- **A directory symlink/junction under Drivers copied as a broken, silently empty 0-byte file** - Ken
  reported this independently, having noticed the same thing while investigating (specifically
  "hardlinks to folders"; Windows junctions/directory symlinks are what that colloquially refers to).
  Root cause: `filepath.WalkDir` does not follow a symlink it encounters - Go reports it as a small
  non-directory entry regardless of what it actually points to - so `copyTreeMerge` opened the link's
  own path expecting file bytes, which created the destination file (truncating it to empty first)
  before failing to read anything from what is actually a directory. Confirmed live against a real
  Drivers folder that aliases several macOS version folders to one real shared driver folder this way
  (`macOS/Canon/15-Sequoia` -> `26-Tahoe`, etc., to avoid keeping duplicate copies locally). exFAT (what
  a flash drive is always formatted as) has no symlink/junction support at all, so there's no way to
  preserve the link itself across the copy - `copyTreeMerge` now follows any symlink/junction it finds
  (`collectCopyJobs`, replacing the plain `filepath.WalkDir`) and copies its resolved real content in
  its place instead, duplicating bytes across every alias of the same target rather than leaving a
  broken stand-in. Guards against a link that resolves to one of its own ancestors (a genuine cycle)
  rather than recursing forever.

## 2026-09-09 - Fixed: runaway self-nested MSI extraction (Canon DiasSetup)

Found while investigating further on-launch scan speedups after v0.4.0 shipped: `ensureMsiExtracted`
(`internal/driver/msi.go`) walked a manufacturer folder for `.msi` files with no bound on how deep it
would chase them. Canon's `DiasSetup.msi` (a bundled device-status-monitor utility, unrelated to the
actual PCL6/PS3/UFRII print driver `.inf`s) administratively installs a verbatim copy of itself one
level into its own output - apparently for its own uninstaller's use - and `ensureMsiExtracted` had no
way to recognize that nested copy as "the same package already handled." Every fresh app run (each
`wails dev`/`go test` invocation against a real local Drivers folder during development) found that
leftover nested `.msi` as new, unextracted work and extracted it again, nesting one level deeper -
forever, with no bound. Confirmed live on this dev machine: 12-13 levels deep across all three Canon
packages (PCL6/PS3/UFRII, both 32BIT and x64 - six instances total), ~310MB and 258 files of pure
duplication, containing zero `.inf` files the catalog scan could ever have wanted.

Fixed by having `ensureMsiExtracted` skip descending into any directory that is itself a prior
extraction's destination (a directory whose name plus `.msi` exists as its own sibling file) - it never
needs to hunt for more `.msi` packages inside output it already produced. Measured impact on this
nVME-backed dev machine was small (BuildCatalog: ~935ms -> ~899ms) since a few hundred extra file stats
barely register at nVME speeds - the real payoff is that this can no longer silently keep growing worse
with every run, and it meaningfully cuts the file/byte count that would otherwise get copied onto and
scanned from a real USB flash drive, which is exactly the slow-media case v0.4.0's own USB fix targets.
Worth checking any already-prepared local installs/USB drives for the same bloat (`Drivers\Windows\11\
Canon\*\*\misc\DiasSetup\DiasSetup\` nested further than one level) - this fix only stops it from
growing further, it doesn't retroactively clean up copies that already exist elsewhere.

## 2026-09-09 - Fixed: slow USB on-launch scan, JSON save not propagating

Two issues Ken observed using PDT at a real client site (Windows, on-site deployment), root-caused and
fixed in this Windows-side session (picking up after the macOS-port session that filed the original
field report below).

- **On-launch Drivers scan is slow from a USB flash drive, especially over USB 2.0** - observed as a
  series of `C:\Windows\System32\expand.exe` console windows flashing up during startup. Root cause was
  exactly Ken's own second hypothesis: `loadCatalog` (`app_windows.go`) unconditionally called
  `driver.BuildCatalog`, which runs every `ensure*Extracted` helper (`internal/driver/catalog.go`'s
  `scanManufacturerFolders`) - including a full `filepath.WalkDir` and an `expand.exe` invocation per
  Lexmark `.msi` - on **every app startup and every Refresh Drivers click**, regardless of whether
  anything actually needed extracting. Fixed by adding `driver.BuildCatalogNoExtract` (same `.inf`
  scan, but skips every `ensure*Extracted` call outright) and having `loadCatalog` call it instead of
  `BuildCatalog` whenever this exact running `PDT.exe` sits on a removable drive
  (`flashdrive.IsRemovableDrive`, already used elsewhere to gate Write to Flash Drive). Matches the
  intended real-world deployment exactly as Ken described: flash drives carry a physical write-protect
  switch and are only ever written to from a technician's local install, so a USB-run copy can safely
  assume everything on it is already extracted. `postSyncDriversHook` (Write to Flash Drive/Sync) is
  unchanged - still always runs the full extracting `BuildCatalog`, since that's the one path that
  should extract. A local/fixed-drive install is likewise unaffected - still gets the full extracting
  scan Ken measured at ~10 seconds on his own nVME dev machine.
- **Editing and re-saving a JSON configuration didn't propagate a fix to other endpoints** - Ken loaded
  a saved configuration on one endpoint, noticed a typo in a printer object's name, fixed it, used Save
  Configuration to overwrite the same file, then loaded what he expected to be the corrected file onto
  other endpoints - the typo was still there. Confirmed Ken's own hypothesis: `OpenConfiguration` and
  `SaveConfiguration` (`app.go`) independently defaulted their dialogs to `configsRoot()` +
  `<SalesChainID>.json`, with zero memory of the path a configuration was actually opened from - so
  re-saving after a small edit silently created a second file in `configsRoot()` instead of overwriting
  the one actually being distributed to other endpoints, unless the technician happened to navigate
  Save's dialog back to the exact original file by hand every time. Fixed by tracking the most recent
  Open/Save path (`App.lastConfigPath`) and having `SaveConfiguration` default its dialog back to that
  exact file/folder when one is known, falling back to the old `configsRoot()`/SalesChainID-based
  default otherwise (e.g. the first save of a brand-new configuration). A new `ResetConfigPath` method,
  wired into the frontend's Reset action, clears this memory so a freshly-reset session doesn't default
  Save back to whatever was open before the reset.

## 2026-09-09 (v0.4.0) - macOS support (data layer, darwin Deployer, Wails app, frontend)

The first real macOS build - PDT is no longer Windows-only. Built and verified on a real Mac (macOS
26 "Tahoe", Apple Silicon) against real vendor driver packages (Kyocera, Ricoh, Sharp) and this
machine's own already-deployed CUPS queues, not just unit tests. `internal/printer.Deployer` - already
a clean, platform-independent interface from the original Windows-only design - now has a second real
implementation alongside `internal/printer/windows`.

### Added
- **macOS driver catalog** (`internal/driver/maccatalog.go`, `macmount.go`, `macresolve.go`): scans
  `Drivers/macOS/<Manufacturer>/<any version folder>/*.dmg`/`*.pkg` (mirroring the Windows side's own
  `Drivers/Windows/<version>/<Manufacturer>/` convention, version nested the other way per the
  README's own longstanding note that macOS packages genuinely vary by OS release), plus a flat
  `Drivers/macOS/OpenPrinting/<Manufacturer>/*.ppd` fallback bucket for a manufacturer/model with no
  vendor installer at all. `driver.LocatePkg` resolves a `.dmg` to the real `.pkg` inside it
  (mounting via `hdiutil`, recursing into one level of nested `.dmg` - confirmed necessary against a
  real Kyocera package) with no bundled extraction tool needed (unlike Windows' bundled 7-Zip - macOS
  driver packages need no pre-extraction at all).
  - **No reliable per-package version field exists on macOS**, unlike a Windows `.inf`'s `DriverVer=`
    line - confirmed against a real Kyocera distribution-style package, where every component's own
    declared "version" was boilerplate `1.0`/`0`. `ResolveMac` picks the newest package by file
    modification time instead of trying to compare a version string; `PackageLabel` is a display-only
    best-effort label (a flat `.pkg`'s real `PackageInfo` version when there is one, else the
    filename), never used for ordering.
- **darwin `Deployer`** (`internal/printer/darwin`): installs a resolved `.dmg`/`.pkg` via macOS's own
  `installer` tool (elevated - see below), diffs `/Library/Printers/PPDs/Contents/Resources` before
  and after to discover which PPD(s) the install actually registered (there's no Windows-registry-like
  "installed driver version" to read directly), and creates/reuses a CUPS **LPD** print queue
  (`lpadmin`/`lpstat`) - `lpd://<ip>/`, no queue name, confirmed against this machine's own
  already-deployed real queues to be exactly the working convention already in use here. Best-effort
  duplex/color defaults are set by reading each queue's actual PPD-declared option keywords/choices
  (`lpoptions -l`) rather than hardcoding one vendor's naming - confirmed against a real installed
  Kyocera PPD (`Duplex`/`None`/`DuplexTumble`/`DuplexNoTumble`, `ColorModel`/`CMYK`/`Gray`) that PPD
  option naming is inconsistent enough across vendors that this has to be dynamic, the same lesson
  `devmode_windows.go` already learned the hard way for DEVMODE on Windows. No NUL:-port workaround and
  no APF/"print spooled documents first" - both Windows spooler-specific concepts with nothing
  analogous in CUPS (see `deploy_darwin.go`'s own doc comment for why).
- **Elevation**: every privileged command (`installer`, `lpadmin`) runs through `osascript`'s
  `do shell script ... with administrator privileges` - the closest available equivalent to Windows'
  manifest-driven auto-UAC-elevation without needing a paid code-signing certificate. **Confirmed live
  that a bare, ad-hoc-signed CLI binary gets killed by AMFI** (`AppleMobileFileIntegrityError -423`,
  "adhoc signed or signed by an unknown certificate chain") the moment the privileged command actually
  starts, even after the password prompt is accepted - a real `wails build`-produced `.app` bundle
  (also only ad-hoc signed today, no paid Developer ID) has not yet been confirmed to avoid the same
  rejection; see `elevate_darwin.go`'s own doc comment. If it turns out a real `.app` hits this too,
  the fix is a paid Apple Developer ID signature (and likely notarization) - a real cost/process change
  from this project's current unsigned-installer precedent on the Windows side.
- **`cmd/pdtdebugmac`**: a throwaway CLI mirroring `cmd/pdtdebug`'s own role, for exercising the darwin
  catalog/install/queue-creation codepaths by hand against real state before the Wails UI could.
- **`package main` now actually compiles and runs on darwin** - `app.go`'s Windows-only pieces
  (driver-catalog methods, the self-update mechanism, the Explorer-opening toolbar button) split into
  `app_windows.go`/`drivercatalog_windows.go`/`update_windows.go`/`openfolder_windows.go`, each with a
  matching `_darwin.go` counterpart where one makes sense; `spooler.go`/`devmode.go`/`sevenzip.go`
  renamed outright to `_windows.go` (Print Spooler control, DEVMODE/Device Settings capture, and the
  bundled-7-Zip-for-self-extracting-archives tooling all remain Windows-only for now - no CUPS/macOS
  equivalent built yet). New `App.Platform()` bound method (`runtime.GOOS`) is the frontend's only
  feature-detection signal.
- **Flash Drive / Sync ported to macOS** (`internal/flashdrive/flashdrive_darwin.go`): removable-drive
  enumeration and exFAT formatting via `diskutil` (`RemovableMediaOrExternalDevice` from
  `diskutil info -plist`, converted to JSON via `plutil` for reliable parsing) and `syscall.Statfs` for
  free/total space, in place of Windows' `GetDriveType`/`GetDiskFreeSpaceEx`. Same `flashdrive.Drive`
  shape on both platforms - `Letter` holds a mount point path (e.g. `/Volumes/MYDRIVE`) on macOS
  instead of an actual drive letter, since every caller already treats it as an opaque identifier
  string rather than parsing it.
- **Frontend** (`frontend/src/main.js`): a `state.platform`-gated reduced UI on macOS - Spooler,
  SNMP/port-prefix/Use-existing-port, APF, DEVMODE capture, Import Printers, app self-update, and the
  7-Zip credit/update section are all hidden (Windows-only concepts, per above); the Driver combobox
  stays (now backed by `App.DriverCandidates`/`DefaultDriverFor`'s own darwin implementation - the
  resolved package's own label when one exists locally, or fuzzy-ranked OpenPrinting PPD labels
  otherwise). Switched from named imports of the generated Wails bindings to a namespace import
  (`import * as App from ...`) - confirmed live that Vite/Rollup hard-fails the production build on a
  named import of a bound method that doesn't exist in that platform's generated `App.js` (a fair
  number of `App`'s own methods only exist on one platform now), where a namespace import's member
  access degrades to `undefined` at runtime instead, caught by the same `isMac()` guards already
  needed at every one of those call sites.
- User-visible "SalesChain ID" wording changed to "Save ID" throughout (tooltips, log/status messages,
  confirmation dialogs) - internal identifiers and the saved-JSON-config field name are unchanged, so
  this doesn't affect config file compatibility.

### Fixed
- Two pre-existing test gaps, invisible until `package main` could actually compile/run on darwin for
  the first time this pass: `resolveexerelative_test.go` hardcoded Windows-only path syntax
  (`C:\Users\...`, `.\Drivers`) for a function that's genuinely cross-platform; and
  `TestResolve_StaleVersionFallsBackToNewest` (`internal/driver`) was missing the same amd64-only
  `t.Skip` its two sibling tests already had, failing on Apple Silicon since the real-vendor-INF test
  fixtures it uses have no arm64 build for that particular old Kyocera model.

### Known issues / open for the next pass
- **Settings > General's Browse (`...`) buttons don't open a folder picker on macOS** - not yet
  root-caused; `PickFolder`/`runtime.OpenDirectoryDialog` is nominally cross-platform Wails runtime
  code, unmodified here.
- **Settings > General's base path fields display Windows-style `\`-separated paths on macOS** -
  `defaultDriversBasePath`/`defaultSaveFileBasePath`/`defaultPreinstallBasePath` (`settings.go`) still
  need a macOS-appropriate default location decided and implemented (the Windows side defaults to
  `%LocalAppData%\PDT\Drivers` etc.); until then, paths display with the wrong separator and may not
  point anywhere sensible on macOS.
- No macOS equivalent of `ensureDriversScaffold` yet (auto-creating the `Drivers/macOS/<Manufacturer>/`
  tree on first launch) - a freshly-installed macOS copy's Drivers folder isn't scaffolded the way an
  installed Windows copy's is.
- A minor Defaults-panel spacing/padding issue above the Manufacturer row, reported after the fixes
  above landed - not yet reproduced/fixed.
- `PrinterRow` has no explicit LPD-queue-name field - every macOS deploy uses `lpd://<ip>/` (no queue
  name), confirmed to match this machine's own real, already-deployed queues, but not yet exposed as a
  per-row override if a future printer's LPD server needs a specific queue name.

## 2026-09-07 (v0.3.9) - Toolbar Sync button; fix and speed up flash drive copying

### Added
- **Toolbar Sync button** (the two opposing horizontal arrows, between Refresh and the Drivers-folder
  button) copies this computer's Drivers folder onto a flash drive that already has a portable PDT
  copy on it, then extracts anything newly-copied right there - for topping up a flash drive with
  driver packages downloaded since it was last written, without rewriting the exe/Configs/tools or
  waiting to plug it into another computer first just to trigger extraction. Available whether PDT
  itself is running from a local install or from a flash drive already (unlike Write to Flash Drive,
  which is disabled in the latter case) - syncing one portable copy's Drivers onto a *different* flash
  drive is a legitimate workflow, guarded against picking the exact same drive PDT is currently running
  from (`syncDriversTo`'s own `samePath` check - copying a tree onto itself would truncate a source
  file while still reading it).
- **Write to Flash Drive now also copies this computer's 7-Zip tools folder** onto the destination, so
  a portable copy is fully self-contained and needs nothing else to work on another computer.
- **Copy-progress dialog** for both Write to Flash Drive and Sync - a real Drivers folder can be tens
  of thousands of files and take several minutes over a real USB port with otherwise zero indication
  it hadn't just hung (confirmed live). Shows which step (Drivers/Configs/7-Zip tools) is currently
  copying and a live "N / total files" progress bar per drive, via a new `flashcopy-progress` Wails
  event emitted throughout the copy (throttled to at most once per 150ms so tens of thousands of files
  don't flood the frontend with events).

### Fixed
- **Write to Flash Drive silently copied none of the real driver files onto a flash drive that already
  had anything under `Drivers\`** (a prior Write to Flash Drive, or its own scaffolded
  `Archive\README.txt` files) - confirmed live, and root-caused to `os.CopyFS`'s own documented
  behavior: "will not overwrite existing files ... stops at and returns the first error encountered."
  Replaced with `copyTreeMerge`, which merges into an already-populated destination instead of failing
  outright on the first pre-existing path it finds.
- **A second, related bug found while fixing the first one**: `copyTreeMerge`'s own first version still
  aborted the *entire* copy the moment any single file failed for any other reason (`filepath.WalkDir`'s
  default behavior) - confirmed live against a real flash drive, where a file deep in Lexmark's own
  driver package (thousands of files, deeply nested - a plausible Windows `MAX_PATH` issue) failed, and
  every manufacturer sorting after Lexmark (Ricoh, Sharp, Toshiba, Xerox) never got copied at all as a
  result, with nothing to explain why. `copyTreeMerge` is now best-effort per file (one bad file no
  longer stops the rest of the tree), and `writePortablePDTTo`/`syncDriversTo` now attempt every step
  (Drivers, Configs, tools) regardless of whether an earlier one hit a partial failure, instead of
  bailing out of the whole operation on the first error.
- **Repeat Write to Flash Drive/Sync against an already-populated drive was extremely slow** - tens of
  thousands of individual `os.Stat` round-trips to a real USB-attached filesystem, one per source file,
  turned out to be the dominant cost once the two bugs above were fixed and files actually started
  landing. Replaced with a single bulk directory listing of the destination up front
  (`listFileSizes`) - a repeat sync of an already-fully-synced ~23,000-file, ~14GB real Drivers folder
  went from several minutes to about 4 seconds. CRC32/MD5 content hashing was considered for detecting
  changed files and rejected: computing a hash means reading every byte of every file on both sides,
  which costs far more I/O than the size-comparison it would replace, for a correctness guarantee this
  case doesn't need - driver packages are downloaded once and never silently modified in place
  afterward, so a size match is already as good as a hash match here.
- Fixed the Sync modal's "Format as exFAT" row staying visible despite being correctly hidden under the
  hood - another instance of this codebase's recurring `[hidden]`-vs-bare-`display` CSS specificity bug
  (see `.modal-backdrop`'s own comment), this time on `.modal-field-inline`; also switched that one
  element to an inline `style.display` toggle instead, since the row still rendered even with the
  compiled bundle's own logic and the CSS fix both verified correct byte-for-byte - moot with the
  belt-and-suspenders fix in place either way.

## 2026-09-07 (v0.3.8) - Startup overlay; portable flash drives now fully self-contained

### Added
- **Startup overlay** ("Initializing...", with a spinner) covers the whole app from the moment it
  renders until `init()` finishes wiring up every event listener - previously that whole span (easily
  noticeable when `BuildCatalog` has real extraction work to do) had zero event listeners attached at
  all, so clicking anything, Settings included, did nothing with no indication why. Confirmed live with
  an artificial delay that the overlay blocks input the entire time and disappears the instant the app
  is actually ready.
- **Write to Flash Drive's toolbar button is now disabled when PDT itself is running from a removable
  drive** (`IsRunningFromRemovableDrive`, `flashdrive.IsRemovableDrive`) - confirmed live this used to
  fail with "The process cannot access the file because it is being used by another process" trying to
  overwrite its own running exe; only an installed copy on a technician's laptop is a sensible source
  for stamping out more portable copies anyway.
- **A portable copy's Settings now shows and uses relative base paths** (`.\Drivers`, `.\Configs`)
  instead of an absolute path baked to whatever drive letter the flash drive happened to have at the
  time - a real gap, since a flash drive doesn't keep the same letter across computers, or even across
  relaunches on the same one. `resolveExeRelative` resolves a relative Settings path against wherever
  the exe is *currently* running from, at every point of use (`driversRoot`/`configsRoot`, `PickFolder`,
  Open/Save Configuration's own starting directory, the toolbar's Drivers-folder button) - an absolute
  path (an installed copy's `%LocalAppData%\PDT\...`, or anything explicitly chosen via Browse) is
  unaffected.

### Fixed
- **Write to Flash Drive left the destination missing its Configs folder entirely**, and missing
  Drivers subfolders for any manufacturer not already downloaded on the technician's own laptop -
  confirmed live. A technician's local Configs folder very often doesn't exist yet (nothing saved or
  captured there so far) and their local Drivers folder is rarely fully populated for every
  manufacturer PDT knows about; `writePortablePDTTo` now guarantees both a Configs folder and a full
  manufacturer scaffold exist on the flash drive regardless of what the source had.
- **Settings > General's "Manufacturer sort order (Alphabetize)" link moved next to its label**
  (previously sat on its own line below it, from when it was first added).
- **Settings > External Sites' text fields butted straight up against the tab's own scrollbar** with
  no visual gap at all once there were enough manufacturers to make it scroll - added 8px of padding.

## 2026-09-07 (v0.3.7) - Fix disabled format-warning buttons; USB icon for Flash Drive

### Fixed
- **Write to Flash Drive's "Erase and Format" warning had both its buttons disabled whenever Save ID
  was empty.** Write to Flash Drive itself is deliberately job-independent (exempt from the Save ID
  gate - see `applySalesChainGate`'s own doc comment), but the shared confirm dialog it pops up through
  (`showConfirm()`/`#confirmBackdrop`) lived outside that exemption, so its own OK/Cancel buttons still
  got swept and disabled - confirmed live, with a real removable drive, that both buttons were
  unusable with an empty Save ID before this fix. `#confirmBackdrop` is now exempt too; the two other
  callers of `showConfirm()` (Reset Configuration, Export Configs) are themselves gated by the same
  sweep, so their own confirm popups were already unreachable while locked either way - this changes
  nothing for them.

### Changed
- **Flash Drive toolbar icon changed again** - the 🖴 swapped in last release still didn't read as a
  flash drive to actual use. Replaced with a hand-drawn inline SVG of the classic USB trident symbol
  (circle/square/triangle branches over a plug shape) instead of gambling on a third emoji glyph -
  guarantees the exact appearance regardless of font/emoji rendering differences across machines.

## 2026-09-07 (v0.3.6) - Fix appwiz.cpl self-update sync, retroactive Kyocera repair, UI polish

### Added
- **Settings > General's Manufacturer sort order gains an "Alphabetize" link** next to the label,
  sorting the reorderable list A-Z in one click rather than dragging every entry by hand. Operates on
  whatever's currently shown (including an unsaved drag reorder already in progress), the same way the
  list's own drag-and-drop only takes effect once Save is clicked.
- **Self-updating through Settings > About now keeps Programs and Features (appwiz.cpl) in sync.**
  `ApplyUpdate` previously only replaced the running `PDT.exe` in place - the Inno Setup uninstall
  entry's `DisplayVersion` (all appwiz.cpl's own Version column ever reflects) was untouched, so it
  kept showing whichever version the installer itself last ran, even after a self-update moved the
  actual exe ahead of it. `ApplyUpdate` now also best-effort updates that registry value (tries both
  `CURRENT_USER` and `LOCAL_MACHINE`, since exactly one holds it depending on how PDT was installed;
  a portable/flash-drive copy has neither, silently a no-op) - confirmed live against this project's
  own installed copy, `appwiz.cpl`'s Version column moved from 0.3.4 to 0.3.5 with no reinstall.

### Fixed
- **v0.3.5's fix only prevented the Kyocera raw-PE-dump bug from recurring - it didn't repair an
  install that already had one sitting around from before that fix existed.** A leftover wrong folder
  (created by the bug this same release already documents) still satisfied
  `kyoceraVersionAlreadyExtracted`'s substring check, so the correct extraction kept getting skipped
  even on an install upgraded to carry the v0.3.5 fix - confirmed live against this project's own
  actual installed copy (`%LocalAppData%\Programs\PDT`), not just its dev build. `ensureKyoceraExesExtracted`
  now recognizes this exact leftover (a top-level `.text` *file*, not a folder - `looksLikeRawPEDump`)
  and removes it before deciding whether a version still needs extracting, so upgrading to this fix
  actually self-repairs an already-poisoned install rather than requiring a manual folder deletion.

### Changed
- **Settings dialog +80px wider, +50px taller** (`.settings-modal` 550px -> 630px; `.tab-panel` height
  385px -> 435px) - on top of v0.3.3/v0.3.4's own bumps, for the new Alphabetize link and more general
  breathing room.
- **Flash Drive toolbar button no longer looks like a floppy disk.** Swapped the icon from 💾 (floppy
  disk) to 🖴 (a flat rectangular drive shape) - the feature itself (Write to Flash Drive) was never
  about floppy disks, and the old icon was confusing at a glance.

## 2026-09-07 (v0.3.5) - Fix Kyocera extraction regression from generalized SFX detection

### Fixed
- **Generalizing self-extracting-archive detection to 7z/Zip (v0.3.3) broke Kyocera's own two-stage
  extraction** - a Kyocera driver package's raw bytes do contain a real archive signature within
  `ensureSfxArchivesExtracted`'s scan window (its `.text` PE section IS the embedded archive, just not
  appended cleanly after the PE stub the way RAR/7z/Zip SFX packages are), so it was extracting
  Kyocera's `.exe` too - wrongly, into a same-named sibling folder containing nothing but raw PE
  sections (`.text`/`.rsrc`/`.reloc`/`CERTIFICATE`, not a single real driver file). Worse, that wrong
  folder's name then satisfied `kyoceraVersionAlreadyExtracted`'s own substring check, permanently
  blocking `kyoceraexe.go`'s correct two-stage extraction from ever running for that version again.
  Fixed by having `ensureSfxArchivesExtracted` skip any Kyocera-named `.exe` outright (`kyoceraExeNameRe`)
  rather than relying on call order alone, and reordered `scanManufacturerFolders` to run the
  Kyocera-specific extraction first regardless. The bad leftover folder in this project's own Drivers
  folder was removed; a fresh extraction was confirmed byte-identical to the correct one already
  produced by hand earlier.

## 2026-09-07 (v0.3.4) - Settings dialog widened further

### Changed
- **Settings dialog +50px wider, +20px taller** (`.settings-modal` 500px -> 550px; `.tab-panel` height
  365px -> 385px) - on top of v0.3.3's own +40px/+15px bump, for more breathing room around its
  base-path fields and manufacturer URL list.

## 2026-09-07 (v0.3.3) - Live driver catalog refresh; wider toolbar/Settings; retrofit Archive folders

### Added
- **Toolbar Refresh button (🔄)** rescans the Drivers folder in place (`RefreshDriverCatalog`) - no
  more restarting PDT just to pick up a newly downloaded or extracted driver package. The driver
  catalog (`App.catalog`/`modelIndex`) is now guarded by a real lock (`catalogMu`) rather than being
  build-once-at-startup-only, since a `DriverCandidates`/`Deploy` call can now land at the same moment
  as a refresh. The no-drivers banner's wording points at this button instead of suggesting a restart:
  "...once a driver package is downloaded into the Drivers folder, press the Refresh button (🔄) to
  make it available."
- **Self-extracting archive auto-extraction now also handles 7z and Zip, not just RAR** - what used to
  be `internal/driver/rarsfx.go` (`ensureRarSfxExtracted`, RAR-signature-only) is now
  `internal/driver/sfx.go` (`ensureSfxArchivesExtracted`), matching any of the three signatures within a
  candidate `.exe` and letting the bundled `7z.exe` auto-detect the exact format itself. Confirmed live
  against a real Konica Minolta driver package (`KM_UPD_pcl6_win64_...inst.exe`) that ships as a 7z SFX
  stub with the archive simply appended after it - same layout as Lexmark's RAR, different signature -
  extracted correctly with no manufacturer-specific handling needed, unlike Kyocera's own packaging
  (its embedded archive sits inside the `.text` PE section instead, so it keeps its bespoke two-stage
  extraction in `kyoceraexe.go`).
- **Installer's Programs and Features (appwiz.cpl) entry now reads just "Printer Deployment Tool"**,
  not "Printer Deployment Tool 0.3.1" - added an explicit `UninstallDisplayName` (Inno Setup otherwise
  defaults that to AppName + AppVersion). The version is still visible in that same dialog's own
  "Version" column, and the installer wizard's own title bar is unaffected.
- **Toolbar Drivers-folder button (📂)** opens the current Drivers Base Path in File Explorer directly
  from the toolbar (`OpenDriversBasePathInExplorer`, scaffolding it first if it's empty) - Settings >
  General's own equivalent right-arrow button was removed as redundant now that this exists.

### Changed
- **Main window widened 1054px -> 1204px** - the top toolbar had grown (Spooler dropdown, then Refresh
  and Drivers-folder buttons added this same release) to where it was only ~18px away from wrapping
  onto a second line at the old width; confirmed via direct measurement of each button's own on-screen
  position that this now leaves roughly 150px of slack instead, enough headroom to absorb a
  wider-than-usual system font/DPI rendering the button row's own gap can't otherwise account for.
- **Settings dialog +40px wider, +15px taller** (`.settings-modal` override of the shared `.modal`
  width; `.tab-panel` height 350px -> 365px) - its General/External Sites/About tabs carry more
  per-row content than the simple confirm-style dialogs that share the base `.modal` class, which stay
  at their original size.

### Fixed
- **Extracting a self-extracting `.exe` or `.zip` whose own internal content was already a single
  top-level folder produced a redundant `Foo/Foo/...` nesting** instead of the intended `Foo/...` -
  confirmed against a real Konica Minolta package (`KM_UPD_pcl6_win64_...inst.exe`) that packages
  itself this way. `flattenRedundantWrapperDir` (`internal/driver/flatten.go`) now collapses that
  extra level after `ensureZipsExtracted`/`ensureSfxArchivesExtracted` run - but only when the single
  top-level entry's name actually matches the destination folder's own name, so a package whose
  genuine, intentional layout happens to be a single folder (a plain "Driver" subfolder, say) is left
  exactly as it was; a real regression caught in this package's own test suite while implementing this
  is what led to that narrower, name-matched condition instead of a blanket "always hoist a lone
  subfolder" rule. The already-broken KM extraction left over from testing this before the fix
  existed was removed so the next scan re-extracts it correctly.
- Confirmed the toolbar's Refresh button already re-runs every archive auto-extraction step
  (zip/self-extracting-archive/msi/Kyocera), not just re-scanning already-extracted `.inf` files -
  `RefreshDriverCatalog` calls the exact same `driver.BuildCatalog` startup does, so dropping in a
  raw, never-extracted driver package and clicking Refresh is enough on its own. Documented explicitly
  on `RefreshDriverCatalog` itself, since it wasn't obvious this fell out for free.
- **`ensureDriversScaffold` never actually added `Archive` folders to a manufacturer that already had
  real driver packages in it** - the "only scaffold if root is completely empty" check operated on the
  whole Drivers root, so it silently no-opped for every already-populated install, including this
  project's own real Drivers folder. It's now unconditional and idempotent (safe on every startup):
  every manufacturer in `driver.Manufacturers` gets its `Archive` folder and README ensured, whether
  its own top-level folder is brand new or has been populated by hand for months - the one case still
  left alone entirely is an older flat-layout Drivers folder (no `Windows` subfolder at all), so this
  can't accidentally trip `BuildCatalog`'s own flat-vs-nested back-compat detection.
- Archive folder's placeholder file is now `README.txt` (was `README.md`) - a stale `README.md` from
  an earlier PDT version is removed the next time this scaffold step runs. Wording also changed from
  "The script never scans this folder..." to "The program never scans this folder..." (PDT is a
  compiled program, not the PowerShell script - `Create-Printers.ps1` - it replaced).

## 2026-09-07 (v0.3.1) - Zero-driver first run now actually usable; installer launch-after-install fix

### Added
- **PDT is now fully usable on a brand-new install with zero drivers present** - previously, a fresh
  install's empty Drivers folder crashed startup entirely (see Fixed, below); now that the crash is
  fixed, the Manufacturer dropdowns (Defaults panel and each grid row) offer every manufacturer PDT
  knows about regardless of whether any driver is present locally yet, and a new banner above the
  Defaults panel explains the bootstrap workflow: pick a Manufacturer, click **Check for Updates** to
  open its download page, then drop the downloaded package into the Drivers folder. `GetCatalogStatus`
  gained a `HasDrivers` field (distinct from its existing load-error `OK`/`Error`) so the frontend can
  tell "loaded fine, just genuinely nothing here yet" apart from a real catalog failure. The
  Manufacturer dropdown and Check for Updates button are also now exempt from the Save ID gate (see
  `applySalesChainGate`) that otherwise disables nearly everything in PDT until a Save ID is entered -
  picking a manufacturer and opening its download page is job-independent, the same reasoning that
  already exempted Write to Flash Drive and Spooler, and this bootstrap workflow needs to work before
  there's any job to name yet.
- Every manufacturer folder `ensureDriversScaffold` creates now also gets its own `Archive\README.md`,
  matching the real Drivers folders already documented in the "Drivers folder layout" section of this
  README - not just the bare manufacturer folder it created before.

### Changed
- `App.Manufacturers()` (the Defaults panel and grid rows' dropdown source) no longer filters to only
  manufacturers with a driver already present locally - see Added, above, for why. Settings > External
  Sites' `AllManufacturers()` is unaffected (same manufacturer set either way, just alphabetical
  instead of the user's custom order).

### Fixed
- **Installer's "Launch Printer Deployment Tool" checkbox failed with "CreateProcess failed; code
  740. The requested operation requires elevation."** Inno Setup's `[Run]` step defaults to
  `CreateProcess`, which cannot trigger UAC for an exe whose manifest demands elevation (double-
  clicking the installed exe or its Start Menu shortcut already worked fine, since those go through
  `ShellExecute`). Fixed by adding the `shellexec` flag to that `[Run]` entry.
- **A fresh install with an empty (but successfully scaffolded) Drivers folder was completely
  unresponsive** - the SalesChain ID field, every button, all of it, ignored every click and
  keystroke, with no visible error. Root cause: `driver.ManufacturersWithDrivers` returned a nil Go
  slice for "no manufacturers have drivers yet," which marshals to JSON `null` (not `[]`) across the
  Wails bridge; `state.manufacturers.map(...)`, building the Manufacturer `<select>`'s options, threw
  on that `null` as the very next line in `init()`, aborting the rest of startup before
  `wireEvents()` - which attaches literally every event listener in the app - ever ran. Fixed at the
  source (`ManufacturersWithDrivers` now returns `[]string{}`), then swept for and fixed the same
  nil-slice-across-the-bridge pattern in six more spots reachable via ordinary "nothing found"
  outcomes: `driver.Candidates` (unknown manufacturer), `config.ImportCsv` (nothing to import),
  `flashdrive.EnumRemovableDrives` (no removable drives mounted), `exportconfigs.go`'s
  `matchingPreinstallFolders`/`matchingConfigFiles`/`CheckExportCollisions`, and
  `devmode.go`'s `EnumerateLocalPrinters` (enumeration failure).

## 2026-09-07 (v0.3.0) - Windows installer; Drivers/Configs now live in %LocalAppData%\PDT

### Added
- **Windows installer** (`installer/pdt.iss`, built with Inno Setup 6 - `iscc installer\pdt.iss`),
  published as a GitHub Release asset (`PDT-Setup-<version>.exe`). Deliberately packages only
  `PDT.exe` - no `Drivers` folder, which would bloat it for no benefit (driver packages are hundreds
  of MB each) since PDT already scaffolds an empty `Drivers\Windows\11\<Manufacturer>\` structure on
  first launch regardless. **Elevation-optional by design**
  (`PrivilegesRequired=lowest`/`PrivilegesRequiredOverridesAllowed` + `DefaultDirName={autopf}\PDT`):
  double-clicking it normally installs unelevated, no UAC prompt, to
  `%LocalAppData%\Programs\PDT`; explicitly running it as administrator installs to
  `%ProgramFiles%\PDT` instead. Live-verified end to end for the elevated path (silent install landed
  in `C:\Program Files\PDT`, Start Menu/Desktop shortcuts and an HKLM uninstall entry created
  correctly, launching the installed copy correctly scaffolded
  `%LocalAppData%\PDT\Drivers\Windows\11\<Manufacturer>\` for every manufacturer, and the generated
  uninstaller removed everything cleanly); the unelevated path relies on Inno Setup's own
  well-documented `{autopf}` mechanism and could not be directly exercised in this sandboxed session
  (repeated attempts to force it via `/CURRENTUSER` from an already-elevated automated shell hung,
  most likely an artifact of that shell having no interactive desktop session to de-elevate into,
  not a script defect) - worth a real-world confirmation on an ordinary desktop session.
- **Settings > General gains "Drivers Base Path"** (label, text field, Browse button, and a
  right-arrow button that scaffolds-if-empty then opens the folder in File Explorer), positioned
  between Configuration Files Base Path and Preinstall Base Path. Unlike Preinstall Base Path (a
  convenience pointer only), this is a **live** setting: `BuildCatalog` now reads
  `Settings.DriversBasePath` directly, so it actually controls where PDT loads its driver catalog
  from (takes effect after restarting PDT, since the catalog only scans once at startup).

### Changed
- **Drivers and Configs for an installed (non-portable) copy of PDT now default to
  `%LocalAppData%\PDT\Drivers` and `%LocalAppData%\PDT\Configs`**, regardless of whether PDT itself
  was installed under `%ProgramFiles%` or `%LocalAppData%\Programs` - neither is guaranteed writable
  by an ordinary user (`%ProgramFiles%` never is), so a single, always-writable, always-the-same
  location was chosen over forking behavior by install mode. `defaultDriversBasePath`/
  `defaultSaveFileBasePath` (`settings.go`) both still prefer a real, already-populated `Drivers`
  folder sitting next to the running executable first (the portable/flash-drive case), matching
  existing behavior exactly for that case - only a freshly-installed copy with no such folder falls
  back to the new `%LocalAppData%\PDT` default. **Configuration Files Base Path is now also a live
  setting** the same way (`configsRoot()`/`driversRoot()` now read `Settings.SaveFileBasePath`/
  `DriversBasePath` instead of a hardcoded exe-relative-only computation), and takes effect
  immediately within the same session (DEVMODE capture, Export Configs, Write to Flash Drive) rather
  than needing a restart.
- Write to Flash Drive already wrote `Drivers`/`Configs` as siblings of the copied executable on the
  destination drive regardless of the source machine's own `driversRoot()`/`configsRoot()`
  resolution (confirmed unchanged, no code needed) - so a portable copy stamped out from an installed
  PDT (now reading from `%LocalAppData%\PDT`) still gets a normal `<drive>:\Drivers`,
  `<drive>:\Configs` layout, unaffected by where the source machine keeps its own copies.
  Preinstall Base Path is untouched by this entirely, as it should be - it's a technician's own
  laptop-local site-survey folder, never something that belongs on the flash drive itself.
- Settings dialog is 30px taller (each tab's fixed content height: 320px -> 350px), to fit the new
  Drivers Base Path field without scrolling.

### Verified
- New tests: `TestInstalledAppDataDir`, `TestEnsureDriversScaffold_CreatesManufacturerFolders`,
  `TestEnsureDriversScaffold_LeavesNonEmptyRootAlone`. Full suite (`go build`/`vet`/`test`,
  `wails build`, `iscc installer\pdt.iss`) clean.
- Live end to end as described above under "Added" - real silent install/uninstall cycle via the
  compiled installer, not just a review of the `.iss` script.

## 2026-09-07 (v0.2.1) - Kyocera driver packages now auto-extract on startup

### Added
- **Kyocera self-extracting `.exe` driver packages now auto-extract on PDT startup**
  (`internal/driver/kyoceraexe.go`, `ensureKyoceraExesExtracted`) - manually verified live against a
  real ~250MB package before automating it: 7-Zip can pull the embedded driver archive straight out of
  the `.exe`'s own `.text` PE section without ever launching Kyocera's installer, then that extracted
  `.text` file is itself a normal archive, extracted the same way a second time. `BuildCatalog` now
  runs this for every `Drivers\Windows\<version>\Kyocera\` folder it scans, alongside the existing
  `ensureZipsExtracted`/`ensureRarSfxExtracted`/`ensureMsiExtracted` steps: finds every `.exe` matching
  Kyocera's current naming, pulls its version token out of the filename, and skips it if a sibling
  folder's name already contains that version (so it's a one-time cost per driver version, not a
  redo-every-launch one) - same bundled-`7z.exe`/no-op-if-`SevenZipPath`-unset/best-effort-cleanup-on-
  failure conventions as `ensureRarSfxExtracted`. README's "Kyocera" section now documents this as
  Method 1 (drop the `.exe` in and start PDT once - no scratch folders, no running the installer), with
  the two pre-existing manual approaches renumbered to Methods 2 and 3 as fallbacks.

### Verified
- New tests: `TestKyoceraExeNameRe`, `TestKyoceraVersionAlreadyExtracted`,
  `TestEnsureKyoceraExesExtracted_NoOpWithoutSevenZipConfigured`,
  `TestEnsureKyoceraExesExtracted_SkipsAlreadyExtractedVersion`. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean. Confirmed live: the manual two-stage 7-Zip extraction this automates was run by
  hand first, against a real Kyocera KX Driver v8.6A.1412 package, producing the expected
  `32bit`/`64bit`/`arm64`/`Setup.exe`/`KmInstall.exe` layout; PDT.exe then confirmed to start cleanly
  with the new startup step wired in.

## 2026-09-07 (v0.2.0) - Spooler control, per-row SNMP community string, window/label polish

### Added
- **Spooler button** (top bar, between Export Configs and Write to Flash Drive) with a Restart/Start/
  Stop dropdown for the Windows Print Spooler service - `internal/spooler` (`golang.org/x/sys/windows/
  svc/mgr`), a manual escape hatch for a stuck print object or jammed queue, the same fix a technician
  would reach for via services.msc or `net stop/start spooler`. The button itself colors live to match
  the service's actual state: green running, red stopped, yellow while settling (including for the
  duration of Restart's own stop-then-start sequence) - checked once at startup and refreshed after
  every action. Job-independent, so it's exempt from the SalesChain ID gate like Settings/Write to Flash
  Drive.
- **Per-row SNMP community string**: the grid's SNMP column is now a text field, not a checkbox - blank
  disables SNMP monitoring on that row's port, any text enables it and is the community string used
  (`PrinterRow`/`SavedRow` gain `SNMPCommunity` alongside the existing `SNMP` bool). The Defaults panel
  keeps its SNMP checkbox, now paired with its own community field (defaults to "public", disabled until
  checked) used as the template for "Add Printer." A config saved before this field existed (bare
  `Snmp: true`, no community) still infers "public" on load, matching `AddStandardTcpIpPort`'s own
  existing empty-community default instead of silently disabling SNMP for it.

### Changed
- Main window is 30px wider (1024 -> 1054).
- "SalesChain ID" field label reads "Save ID" (the field's id, tooltips, log messages, and the
  `<SalesChainID>-...` filename convention are all unchanged - display label only).

### Fixed
- The Spooler dropdown menu had the same specificity bug this codebase already fixed once for
  `.modal-backdrop`: a plain `.dropdown-menu { display: flex; }` has equal CSS specificity to the
  browser's own `[hidden] { display: none }` and, coming later in the cascade, always won - the menu
  showed permanently open regardless of its `hidden` attribute, making the Spooler button appear stuck.
  Fixed the same way: `.dropdown-menu:not([hidden])`.

### Verified
- `go build`/`vet`/`test` and `wails build` clean. Version bumped to 0.2.0 (`version.go`,
  `wails.json`'s `info.productVersion`).

## 2026-09-07 - Captured DEVMODE/Device Settings, Export Configs, portable flash drives, deploy safeguards

### Added
- **Captured-DEVMODE workflow**, replacing programmatic duplex/color guessing with real driver-produced
  settings replayed verbatim: a per-row "Get DEVMODE" button (green "DEVMODE SET" once captured) tries a
  live capture from a local printer of the same name first, falling back to browsing for an existing
  `.bin`; a toolbar "Get DEVMODE" bulk-captures every checked row; an "Import Printers" dialog lists
  already-configured local printers (physical ones checked by default, software/virtual ones like PDF
  printers unchecked) with an optional immediate capture on import. Captured DEVMODE is stored in a
  `Configs` folder as `<SalesChainID>-<PrinterName>.bin` - the saved JSON config only ever holds a
  filename pointer (`PrinterRow.DevModeFile`/`SavedRow.DevModeFile`), never the raw bytes - and resolves
  at deploy time via `printer.ResolveDevModePath` (explicit pointer first, falling back to the
  conventional filename so forgetting to re-save the JSON after capturing still works). Applied as the
  very last deploy step, after everything else, so nothing else can override it.
- **Device Settings capture**, alongside DEVMODE: most print drivers keep tray assignments and
  installable options (a duplexer, extra trays, a finisher) entirely outside DEVMODE, in a
  `PrinterDriverData` registry key instead (confirmed live: a captured printer's `DeviceOption01`/
  `DeviceOptionSize` values differ from an unconfigured one). `windows.EnumPrinterDataEx`/
  `SetPrinterDataEx`/`CaptureDriverData`/`ApplyDriverData` capture/replay this key verbatim as an opaque
  `<SalesChainID>-<PrinterName>.driverdata.json` sidecar alongside the `.bin`, applied at the same final
  deploy step.
- **Both a per-user AND a global DEVMODE write** (`OpenedPrinter.SetPerUserDevMode` via `SetPrinter`
  Level 2, `SetGlobalDevMode` via the simpler, single-field `PRINTER_INFO_8`/Level 8) - per Microsoft's
  own "Per-User DEVMODE" documentation these are two independent stores Windows never keeps in sync:
  Level 2 is what the General tab's "Preferences" button reads for the calling account, Level 8 is the
  actual admin-set default (Advanced tab's "Printing Defaults", and what Device Settings reads too, for
  most drivers) inherited by any user without their own override. `SetDuplexAndColor`/
  `ApplyCapturedDevMode` now write both, having previously only written Level 2 (nothing else ever
  showed the change) and, briefly, only Level 8 (Preferences stopped reflecting it).
- **Export Configs**: copies every `Configs/<SalesChainID>*` file (saved JSON, captured DEVMODE/driver
  data) to a technician's site-survey folder on their own laptop - locates the
  `"<SalesChainID> - <Client> - <Address>"` subfolder under a new **Preinstall Base Path** setting
  (prompting to pick one if more than one matches), creates a `PDT` subfolder inside it, and copies
  everything over. Warns first that this only makes sense run on the technician's own laptop, and offers
  Overwrite vs. a freshly timestamped subfolder (`PDT\2026-09-06_1622`) if any destination file already
  exists.
- **Write to Flash Drive**: lists currently-mounted USB flash drives (`GetLogicalDrives`/
  `GetDriveType`'s `DRIVE_REMOVABLE`, excluding fixed/network/optical drives), with an opt-in "Format as
  exFAT first" (via PowerShell's `Format-Volume`, with an explicit erase-everything warning naming the
  exact drives) before copying the running executable plus this computer's own `Drivers`/`Configs`
  folders onto each selected drive - the other half of "install PDT once on a laptop, then stamp out
  portable copies."
- **SalesChain ID interaction gate**: every control except the field itself, Open Configuration,
  Settings, and Write to Flash Drive is disabled until SalesChain ID has a value, ruling out ever
  configuring/deploying under the wrong job's ID by mistake. Applied synchronously at first paint (not
  only once `init()`'s async catalog/settings loading finishes) so nothing is briefly clickable before
  the gate takes effect.
- **Required-field yellow highlighting**: SalesChain ID, Manufacturer, and Driver (Defaults panel and
  per row), plus per-row Name (whitespace-only counts as empty) and IP (a real IPv4 address or the
  literal "NUL" - anything else, including a bare subnet prefix missing its last octet).
- **Deploy Checked Printers stays disabled** until every checked row's IP is one of those valid port
  values, decided independently of the SalesChain ID gate (`updateDeployButtonEnabled`) so IP validity
  and SalesChain ID can't fight each other over the button's state.
- **Red STOP button**, enabled only while a deploy is running. Its warning dialog offers a graceful
  **Stop** (cancels between rows - the row already in progress always finishes, since aborting a
  printer/port/driver change half-applied risks leaving it broken) and a **Force Stop** for when PDT is
  genuinely locked up: `App.ForceQuit` immediately terminates the process (`os.Exit`), bypassing every
  graceful-shutdown path on purpose, since a single hung Win32 call has no safe way to be canceled from
  Go once started.
- **Reset Configuration** button: takes the whole app - every row, SalesChain ID, and the Defaults panel
  - back to its fresh-launch state, confirming first whenever there's anything to lose.
- **Double-click (not a confirm dialog) to remove a row** - a stray single click can no longer delete a
  row by accident, without needing a popup for something this frequent.
- An in-app **Warning/Confirm modal** replacing every `window.confirm()` - a native confirm's title bar
  ("wails.localhost says") is fixed browser/WebView2 chrome that can't be reworded or removed, and read
  as an unbranded, out-of-place popup inside an otherwise normal desktop app.
- **Kyocera now gets the create-against-`NUL:`-then-rebind treatment** `RequiresNulPortWorkaround`
  already gave HP's Universal Print Driver family - applied manufacturer-wide (Kyocera has no single
  "universal" driver name to key off the way HP does) after live testing showed Kyocera deploying
  noticeably slower against a live TCP/IP port than Canon or Ricoh; confirmed much faster afterward.

### Changed
- **Model field removed** from the Defaults panel and the grid - the Driver combobox's own text filter
  already does the same narrowing (typing part of a model name finds a model-specific driver name
  directly, which is the only case Model ever mattered for - Kyocera, mainly). The underlying
  `PrinterRow.Model`/`SavedRow.Model` fields still round-trip silently for backward compatibility with
  configs saved before this change; nothing reads them for driver filtering anymore.
- **No more `SalesChain: <id>` printer Comment** - PDT no longer writes this field at all; an existing
  printer's own comment (from before this change, or set by hand) is left alone.
- **"Save File Base Path" renamed to "Configuration Files Base Path"** and now defaults to `Configs\`
  next to the running executable (matching where DEVMODE captures and saved JSON configs already live)
  instead of `Documents\Preinstall`.
- Every frontend-originated log line now timestamps with the same `YYYY-MM-DD HH:MM:SS` format the Go
  side's own log lines use, instead of `toLocaleString()`'s locale-dependent (en-US: `M/D/YYYY, H:MM:SS
  AM/PM`) format, which made the two visibly inconsistent in the same log panel.
- Every one-off status message (a save/load result, a validation warning) now goes to the log panel
  instead of a separate status bar - a long bulk-operation summary ("Captured DEVMODE for N of M checked
  row(s)") could push Deploy Checked Printers onto its own line in the toolbar.

### Fixed
- The SalesChain ID gate's "only re-enable what I disabled" bookkeeping (a `dataset.gateLocked` marker)
  could end up never re-enabling controls at all, depending on call order, when composed with a control
  that had its own independent disable logic elsewhere (`portPrefixText`, disabled both by its own
  checkbox handler and, redundantly, on every `resetDefaultsPanel()`/`wireEvents()` call) - replaced with
  an unconditional, history-independent sweep (every non-exempt control's `disabled` is recomputed fresh
  on every call) plus one explicit re-assertion pass for `portPrefixText`'s own extra condition, rather
  than trying to track "did the gate itself do this."

### Verified
- New tests: `TestDevModeFileName`/`ResolveDevModePath` (+ driver-data equivalents), `TestFileExists`,
  `TestMatchingConfigFiles`/`MatchingPreinstallFolders`, `TestIsPhysicalPrinterGuess`,
  `TestFindManufacturerForDriver`, and updated `TestRequiresNulPortWorkaround` for Kyocera (plus a
  regression case confirming Ricoh's own "UniversalDriver" naming doesn't accidentally match HP's
  manufacturer-specific check). Full suite (`go build`/`vet`/`test`, `wails build`) clean throughout.
- Live end to end on real hardware across this session: Canon, Kyocera, and Ricoh MFDs deployed
  successfully, including the full capture-on-a-reference-printer -> deploy-to-a-fresh-printer DEVMODE/
  Device Settings round trip (confirmed via Printer Properties' Preferences, Printing Defaults, and
  Device Settings tabs all reflecting the captured configuration) and the Configs-folder fallback path.
  Flash drive enumeration confirmed live (correctly reports zero drives with none plugged in); the
  format and copy-to-drive paths are implemented but not yet exercised against real removable media.

## 2026-09-06 - Lexmark default is now the XL driver; 7-Zip credit + self-update in About

### Changed
- **Lexmark's Defaults-panel default is now "Lexmark Universal v2 XL"**, not the base driver -
  `defaultDriverTokens["Lexmark"]` now requires "XL" specifically (alongside "Universal"/"v2", which
  the base driver also matches) so the token match itself picks XL directly, rather than relying on
  the shorter-name tie-break to settle it. The base driver stays fully selectable in the Driver
  dropdown; this only changes which one is pre-filled. Updated `TestDefaultDriverNameFor`'s Lexmark
  case and replaced the Lexmark-specific tie-break test with
  `TestDefaultDriverNameFor_PrefersShorterNameOverNewerUnversionedVariant`, a synthetic,
  manufacturer-agnostic test of the tie-break tier itself (added directly to `defaultDriverTokens` and
  removed after, not real testdata) - it should keep covering that logic regardless of what any real
  manufacturer's own tokens require going forward.

### Added
- **Settings > About now credits 7-Zip** (by Igor Pavlov) - the tool bundled to auto-extract
  self-extracting RAR driver packages - shows the version currently cached, and links to 7-zip.org.
- **Check for 7-Zip Updates / Update 7-Zip Now**, mirroring PDT's own self-update UI exactly: queries
  7-Zip's own GitHub Releases (development now lives at `ip7z/7zip`) via `internal/update.FetchLatest`
  - already generic enough to reuse as-is for a different project's releases, needing only one small
    addition, `Release.AssetMatching(re)`, since 7-Zip's own asset names embed a version number
    ("7z2603-x64.exe") `Asset`'s exact-name lookup can't match. Downloads the latest x64 GUI installer
    and uses the *currently cached* `7z.exe` to pull `7z.exe`/`7z.dll`/`License.txt` back out of it
    directly - confirmed the installer is itself an extractable 7-Zip archive (it can list and extract
    from itself without ever being run as an installer) - then overwrites the cached copies. Extracts
    to a scratch folder first and only overwrites the real cached files once that fully succeeds, so a
    bad download or failed extraction never touches the existing, working files.

### Verified
- New tests: `TestRelease_AssetMatching`, `TestDefaultDriverNameFor_LexmarkXLIsSelectedOverBase`,
  `TestDefaultDriverNameFor_PrefersShorterNameOverNewerUnversionedVariant`. Full suite
  (`go build`/`vet`/`test`, `wails build`) clean.
- Live end to end against the real, current `ip7z/7zip` release: screenshot-confirmed the About tab
  shows "7-Zip 26.03" and "Check for 7-Zip Updates" correctly reports "You have the latest version of
  7-Zip" (accurate - the bundled copy already is 26.03). Since there was nothing newer to test the
  actual download/apply path against live through the UI, called `App.UpdateSevenZip` directly in a
  throwaway test (removed after) with the real, current release's asset URL: it downloaded, extracted,
  and overwrote the cached `7z.exe` successfully (confirmed via the file's updated modtime), then
  confirmed no leftover temp/extraction files remained in the cache folder afterward.
- Screenshot-confirmed the About tab's added content doesn't disturb the Settings modal's fixed size -
  still lands at the same height as before, no scrolling needed.

## 2026-09-06 - Auto-extract .msi drivers too; fix a real default-driver bug it surfaced

### Added
- **`.msi`-packaged drivers are now auto-extracted** (`internal/driver/msi.go`, `ensureMsiExtracted`),
  completing the Lexmark pipeline the previous entry's RAR auto-extraction started: for every `.msi`
  found, runs an MSI *administrative install* (`msiexec /a ... TARGETDIR=...` - unpacks with real
  filenames/paths, installs nothing) into a sibling folder, then decompresses every Microsoft
  legacy-compressed sibling file the install produces via `expand.exe -R` (restore original name,
  read out of the compressed file's own header - not a guessed extension mapping; see the previous
  correction entry for why guessing broke this before). Uses `msiexec.exe`/`expand.exe` directly, no
  bundled tool needed (both are already part of Windows). End to end, dropping the raw, unmodified
  `Lexmark_Universal_v2_UD1_Installation_Package_*.exe` into `Drivers\Windows\<version>\Lexmark\` and
  launching PDT now needs zero manual steps to reach a scannable, installable driver.

### Fixed
- **Real bug, found live once the full pipeline ran against everything in the tree at once**:
  auto-extracting *every* `.msi` surfaced a second real driver - `print64XL.msi`'s
  "Lexmark Universal v2 XL" (an extra-large-format variant), built two days *after* the base
  "Lexmark Universal v2" - and `DefaultDriverNameFor`'s existing "newest date wins" tie-break picked
  the XL variant as the default, which is wrong: it's a different, more specialized product, not a
  newer version of the base driver. Neither name carries a version number of its own (Xerox's/Konica
  Minolta's own tie-break signal doesn't apply here), so the tie-break was reordered to prefer the
  *shorter* matching name before ever considering date - a name that's a superset of another, with an
  extra qualifier tacked on, is presumed to be the more specialized variant. Confirmed the reordering
  doesn't disturb the Xerox/Konica Minolta cases (their own version-number signal is checked first and
  fully resolves both before the shorter-name tier is ever reached).

### Verified
- New tests: `TestCompressedSiblingRe`, `TestEnsureMsiExtracted_SkipsAlreadyExtracted`,
  `TestDefaultDriverNameFor_PrefersBaseNameOverNewerSpecializedVariant` (a new `LexmarkXLPkg` testdata
  fixture reproducing the real two-days-newer-but-wrong-default scenario exactly). Full suite
  (`go build`/`vet`/`test`, `wails build`) clean.
- Live end to end against the real, unmodified Lexmark package: launched the built `PDT.exe` fresh,
  confirmed the full RAR-then-MSI-then-expand chain produced a byte-identical result to the earlier
  hand-verified-correct extraction (same file sizes for `.inf`/`.gdl`/`.gpd`/`.ini`/`.dll`), and
  confirmed via screenshot that the Defaults panel now correctly pre-selects "Lexmark Universal v2"
  (not "...XL") once the tie-break fix was in.

## 2026-09-06 - Bundle 7-Zip, auto-extract self-extracting RAR packages

### Added
- **Self-extracting RAR driver packages (Lexmark's own) are now auto-extracted**, the same way `.zip`
  packages already are. `internal/driver/rarsfx.go`: `isSelfExtractingRar` detects one by scanning for
  the actual RAR signature bytes (not a naming convention - `.zip`-extraction's convention of matching
  on file extension doesn't work here since a self-extracting archive is still just a `.exe`), and
  `ensureRarSfxExtracted` extracts a match into a sibling folder, same skip-if-already-extracted
  convention as `ensureZipsExtracted`.
- **Bundles `7z.exe` + `7z.dll`** (`third_party/7zip/`, embedded via `go:embed` in the new
  `sevenzip.go` and extracted once to `%LocalAppData%\PDT\tools\7zip\` at startup) to actually perform
  that extraction - reached only after ruling out every zero-dependency option:
  - Go's standard library has no RAR reader at all.
  - The one pure-Go RAR library evaluated, `nwaples/rardecode` (`/v2`), opens and lists the real
    Lexmark archive correctly but **silently corrupts exactly the `.msi` files this needs** - confirmed
    by feeding its output back to `msiexec`, which rejected it outright
    (`ERROR_INSTALL_PACKAGE_INVALID`, not just a checksum-mismatch false alarm). No known fix or newer
    version resolves this (checked the library's open GitHub issues directly - nothing matches this
    symptom), and no more-mature pure-Go alternative exists.
  - 7-Zip's own official, easily-redistributable "Extra" console-only package (`7za.exe`) does **not**
    include RAR support at all - confirmed directly by downloading it fresh and testing it against the
    real file (`Cannot open the file as archive`). Only the full `7z.dll` (from the full GUI install)
    has RAR support.
  - Confirmed instead that just `7z.exe` + `7z.dll` (~2.5MB total), copied out of a full 7-Zip install
    with no installer or registry entries, run completely standalone - tested in a bare, isolated
    folder with nothing else present. Redistribution is permitted under 7-Zip's own license (read
    directly from `7-zip.org/license.txt`, not a secondhand summary): the RAR-decoding code is LGPL
    plus an "unRAR restriction" that only bars using it to build a RAR *compressor*, not redistributing
    the decoder - `third_party/7zip/License.txt` ships alongside the binaries per that license's own
    terms.

### Verified
- New tests: `TestIsSelfExtractingRar` (RAR5 and older-format signatures, both at a nonzero offset
  mirroring a real SFX stub, plus a true-negative on an ordinary `.exe`),
  `TestEnsureRarSfxExtracted_NoOpWithoutSevenZipConfigured`,
  `TestEnsureRarSfxExtracted_SkipsAlreadyExtracted`. Full suite (`go build`/`vet`/`test`, `wails build`)
  clean.
- Live end to end against the real, unmodified `Lexmark_Universal_v2_UD1_Installation_Package_*.exe`
  sitting in the real Drivers folder: launched the built `PDT.exe` fresh, confirmed via temporary debug
  logging (removed before finishing) that `ensureRarSfxExtracted` correctly identified the file as a
  self-extracting RAR and extracted it successfully, then confirmed the resulting
  `InstallationPackage\...` folder tree appeared on disk with no manual intervention. (The `.msi` layer
  past that point is still a manual, documented step - see the README's Lexmark section.)

## 2026-09-06 - Lexmark actually works - yesterday's "known limitation" entry was wrong

### Fixed
- **The "Lexmark: known limitation" entry a few sections down was itself the result of two of my own
  mistakes, not a real Lexmark incompatibility** - corrected after redoing the whole investigation
  with a genuinely elevated session (see below) and finding driver install, printer creation,
  duplex/color, and APF all work correctly through PDT's existing, unmodified code. What actually
  happened:
  1. The PowerShell session used for yesterday's testing had silently lost its elevation partway
     through the conversation (confirmed via `whoami /groups` - Medium integrity, Administrators
     group present but "deny only", the signature of a UAC-filtered non-elevated token) - re-tested a
     known-good Canon driver install in that same session and it failed identically, proving the
     session itself was the problem, not Lexmark. Fixed once the user relaunched the terminal as
     Administrator.
  2. Separately, and more subtly: the extraction script guessed each compressed file's real extension
     from its compressed one (`.gd_` -> assumed `.gpd`, `.in_` -> assumed `.inf`) rather than asking
     `expand.exe` for the real embedded name (`expand -R`) - the guess was wrong for both. `.in_`
     actually decompresses to `.ini`, so the guess clobbered the real `.inf` (already correctly
     extracted by the earlier `msiexec /a` step) with unrelated `.ini` content sharing the same
     assumed filename - silently, since both are small text files. `.gd_` actually decompresses to
     `.gdl`, a file the driver's own `CopyFiles` section requires and which never got created at all
     under the wrong assumed `.gpd` name - `SetupCopyOEMInf` tolerated the missing file quietly enough
     at staging time to report success, but `AddPrinter` failed the moment anything tried to actually
     use the driver (`ERROR_CAN_NOT_COMPLETE`), confirmed independently through PDT's own code,
     native `Add-Printer`, and against both the `NUL:` port and a real Standard TCP/IP port (ruling
     out a port-specific cause).
  See the updated "Lexmark: self-extracting RAR + `.msi`-packaged drivers" section in the README for
  the corrected procedure using `expand -R`.

### Verified
- Full round-trip against the real package: `msiexec /a` extraction, `expand -R` decompression of
  every compressed sibling, `EnsureDriverInstalled` (stage + register - PDT's existing
  `driverinstall_windows.go`, no changes), `CreatePrinter` bound to `NUL:`, `SetDuplexAndColor` +
  `GetDevmode` round-trip, `SetAdvancedPrintingFeatures` - all succeeded. Cleaned up afterward
  (test printer deleted, driver package removed via `pnputil /delete-driver`, confirmed gone via
  `Get-Printer`/`Get-PrinterPort`/`pnputil /enum-drivers`) and confirmed nothing was left behind.

## 2026-09-06 - Manufacturer sort order, Lexmark support (metadata only)

### Added
- **Settings > General > Manufacturer sort order**: a plain HTML5 drag-and-drop list (no library)
  controlling the Manufacturer dropdown's order in both the Defaults panel and every grid row.
  `Settings.ManufacturerOrder` persists it; `reconcileManufacturerOrder` (`settings.go`) keeps it a
  valid permutation of `driver.Manufacturers` across saves - preserving the user's own ordering,
  dropping stale names, appending any manufacturer not yet ordered (e.g. one just added to the code)
  at the end rather than letting it silently disappear. `App.Manufacturers()` applies this order
  (`applyManufacturerOrder`); `App.AllManufacturers()` - Settings > External Sites - is deliberately
  exempt and always alphabetical instead.
- **Lexmark** added to `driver.Manufacturers` with a default-driver token rule (`Universal`, `v2` ->
  "Lexmark Universal v2") and a default update-check URL - the catalog-matching/metadata side is fully
  wired up like every other manufacturer. The driver package is a self-extracting RAR archive (not
  zip/7z - can't be auto-extracted the way `.zip` packages are); its `.msi`-embedded files need an MSI
  administrative install (`msiexec /a ... /qn TARGETDIR=...`) plus `expand.exe` to become real,
  correctly-named files `BuildCatalog` can parse.
  **Update, same day**: this entry originally reported actual deployment as a known-broken limitation
  ("the style of the INF is different than what was requested") - that finding was wrong, caused by
  two mistakes on my end (a session that had silently lost admin elevation, and a wrong guess at which
  real filename each compressed sibling file decompresses to), not a real Lexmark incompatibility. See
  the later "Lexmark actually works" entry above for the correction and the "Lexmark:
  self-extracting RAR + `.msi`-packaged drivers" section in the README for the corrected procedure -
  deployment is fully verified working through PDT's existing, unmodified install code.
- Considered and deliberately **excluded Brother**: no universal print driver compatible with most of
  its larger models, and its lineup targets home/small-office rather than fleet deployment.

### Verified
- New tests: `TestApplyManufacturerOrder`, `TestApplyManufacturerOrder_UnlistedItemAppendedAtEnd`,
  `TestReconcileManufacturerOrder_Nil`, `TestReconcileManufacturerOrder_PreservesCustomOrder`,
  `TestReconcileManufacturerOrder_DropsUnknownAndDuplicates` (first tests for the `main` package - it
  had none before), plus `TestDefaultDriverNameFor`'s Lexmark case. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean.
- Screenshot/UI-Automation-confirmed live: with a custom order saved directly to `settings.json`
  (`["Sharp","Ricoh","Kyocera","HP","Canon"]`), the Defaults panel's Manufacturer dropdown correctly
  defaulted to Sharp (with Sharp's own default driver auto-populated), while Settings > External Sites
  still listed all manufacturers alphabetically regardless.
- The Lexmark investigation itself was hands-on against the real downloaded package on this machine,
  not guessed: confirmed the `Rar!` signature by scanning the raw `.exe`'s bytes; confirmed
  `msiexec /a` produces a correctly-named `.inf` and that `driver.DriverNamesFromInf` parses it
  correctly (extracting "Lexmark Universal v2"); confirmed the install failure independently through
  both `pnputil /add-driver` and PDT's own code; checked `setupapi.dev.log` and the `DevicePath`
  registry value to rule out stale driver-store state as the cause; confirmed no partial driver
  registration was left behind afterward (`pnputil /enum-drivers`).

## 2026-09-06 - Toshiba/Xerox/Konica Minolta support; External Sites reverted to plain fields

### Changed
- **External Sites reverted** to yesterday's plain always-visible text field per manufacturer (the
  link-plus-"Change"-modal redesign is gone) - the fixed-size modal made the original design's only
  real problem (a raw URL forcing the window wider) moot, and the simpler always-visible field was
  preferred once that was no longer a tradeoff. `.modal`'s fixed width is kept from that redesign.
- `.tab-panel`'s sizing changed from a hand-tuned `min-height` (needing re-tuning every time External
  Sites' manufacturer count changed - which it immediately did, twice, this same session) to a fixed
  `height` instead: every tab now reserves exactly the same space regardless of its own content,
  permanently, with `overflow-y: auto` for whichever tab has more content than that (now routinely
  External Sites, with 8 manufacturers) rather than ever growing the modal.

### Clarified (no code change)
- The Settings modal stays above the main window and moves with it because it's rendered as an
  in-page overlay (`.modal-backdrop`) inside the same native OS window as everything else - not a
  separate window at all - so there's no separate z-order or position to fall out of sync in the
  first place. Worth keeping in mind for any future change that touches window handles
  (`titlebar_windows.go`'s `findMainWindow`, etc.): this behavior depends on Settings never becoming
  an actual second OS window.

### Added
- **Toshiba, Xerox, and Konica Minolta** added to `driver.Manufacturers`, with default-driver token
  rules (`internal/driver/default.go`) and default update-check URLs (`settings.go`) - see the
  README's "Drivers folder layout" for the full per-manufacturer table.
- **Manufacturers without local drivers no longer appear as deployment options.** `App.Manufacturers()`
  now filters to `driver.ManufacturersWithDrivers(catalog)` (only manufacturers with at least one
  usable driver actually present); a new `App.AllManufacturers()` returns the full, unfiltered list
  for Settings > External Sites specifically, so a URL can still be configured for a manufacturer
  before its drivers are ever added.
- Generalized the "prefer the branded/versioned name over a generic alias" behavior (previously
  hardcoded to Ricoh only - `isUsableDriverName`) into a small per-manufacturer brand-token table
  (`vagueNameFilterBrand`) covering Ricoh, Xerox, and Konica Minolta.
- `DefaultDriverNameFor`'s tie-break, for when more than one catalog name matches a manufacturer's
  token rule, now prefers (in order): the newest INF-declared date, then - on an exact date tie, i.e.
  genuinely the same underlying driver registered under two names - whichever name actually shows a
  version number, then alphabetical only as a final deterministic fallback. Previously this was a
  bare alphabetical tie-break, which happened to work by accident for the original 5 manufacturers
  (each only ever had one name matching their tokens) but picked the *wrong* one once Konica Minolta
  was added: `"KONICA MINOLTA Universal PCL"` sorts before `"...Universal PCL v3.9.13"` since it's a
  literal string prefix of it, so the plain, non-versioned name was winning.
- `foldMatchIgnoringSpaces`: manufacturer folder names now match `driver.Manufacturers` ignoring
  spaces as well as case - found necessary immediately against the real Drivers folder, where
  "Konica Minolta" (this app's display name) sits on disk as `KonicaMinolta`, no space at all.

### Verified
- All three manufacturers tested against the **real, already-populated** local Drivers folder (not
  just synthetic fixtures) via a throwaway catalog-dump script: Toshiba resolves to
  `TOSHIBA Universal Printer 2`, Xerox to `Xerox GPD PCL6 V5.1076.4.0` (correctly preferring it over
  the also-present, unversioned `Xerox Global Print Driver PCL6`), and Konica Minolta - once the
  space-folding fix was in - to `KONICA MINOLTA Universal PCL v3.9.13` (correctly preferring it over
  the plain `KONICA MINOLTA Universal PCL`, an exact-date-tie case the real package actually has).
- New tests: `TestDefaultDriverNameFor_PrefersVersionedNameOnDateTie`,
  `TestBuildCatalog_ManufacturerFolderNameIgnoresSpaces`,
  `TestXeroxGenericAliasIsSelectableButNotDefault`, plus new `Toshiba`/`Xerox`/`KonicaMinolta` (no
  space, deliberately) fixtures under `internal/driver/testdata/`. Full suite (`go build`/`vet`/`test`,
  `wails build`) clean.
- Screenshot/UI-Automation-confirmed live: the Defaults panel's Manufacturer dropdown lists only
  Canon/HP/Kyocera/Ricoh/Sharp/Toshiba/Xerox on this machine (Konica Minolta correctly excluded before
  the space-fold fix, correctly included after); Settings > External Sites lists all 8 with the three
  new URLs pre-filled correctly, confirmed by reading each field's value directly via UI Automation.

## 2026-09-06 - Settings modal no longer resizes when switching tabs; External Sites redesigned

### Fixed
- **Real bug, reported from live use**: switching tabs in the Settings modal visibly resized the whole
  modal on every click - both dimensions, not just vertically as first fixed (see below). Two separate
  root causes:
  - *Vertical*: `.tab-panel` had a `max-height` cap but no lower bound, and the modal has no fixed
    size of its own - it sizes to whichever tab's content is showing. Fixed with a shared
    `.tab-panel` `min-height` (tuned to whichever tab is now tallest - see the External Sites redesign
    below, which changed that).
  - *Horizontal*: `.modal` had a `min-width` but likewise no upper bound, so External Sites' one long
    instructional sentence (a `<p>`, which by ordinary block shrink-to-fit sizing contributes its full
    unwrapped width when nothing constrains its ancestors) stretched the modal to roughly double the
    other tabs' width. Fixed by giving `.modal` a genuine fixed `width` instead of a `min-width`.
  - Confirmed via screenshot: the modal box is now pixel-identical (same edges, same Cancel/Save
    button position) across all three tabs.

### Changed
- **Settings > External Sites redesigned**: each manufacturer is now a clickable link (opens its
  configured URL via the same `OpenManufacturerURL` call Defaults' own "Check for Updates" button
  already used) plus a **Change** button that opens a small "Change URL" modal, instead of an
  always-visible text field showing the raw URL. Saving in that modal persists immediately (its own
  `SaveSettings` call) rather than staying pending for the outer Settings modal's own Save button -
  simplest way to keep the manufacturer link always accurate right after a change, with no separate
  "unsaved edit" state to track just for this one field. This was also what made the horizontal-resize
  bug possible to fully fix rather than just contain: a raw URL string in a fixed-width box either
  clips or forces the modal wider (whack-a-mole either way), and neither problem exists once the URL
  itself isn't shown inline at all.

### Verified
- `go build`/`go vet`/`go test` and `wails build` all clean.
- Screenshot-confirmed: identical modal size across all three tabs; the Change URL modal opens
  pre-filled with the current value, and a save round-trips correctly (confirmed directly by reading
  `%AppData%\PDT\settings.json` before and after, then restoring the real URL afterward).
- Caught and fixed a markup issue while testing the Change URL modal: writing `URL for
  <span id="siteUrlMfgName">` as two adjacent inline pieces directly inside a `flex-direction: column`
  label put them in *separate* flex items (one per contiguous inline run, per the flex layout spec),
  rendering "URL for" and the manufacturer name on two separate lines instead of one. Fixed by wrapping
  both in a single containing `<span>` so they're one flex item.
- Also caught mid-verification (unrelated to the app itself): a Bash heredoc used to restore
  `settings.json` after a manual test collapsed its `\\` path escapes to single backslashes,
  producing invalid JSON ("\U", "\K", "\D" aren't legal JSON escapes). Caught by reading the file back
  before moving on, rather than assuming the write matched what was typed; fixed by rewriting it with
  a tool that doesn't round-trip through shell string interpretation, then re-verified the app starts
  clean and reads it back correctly.

## 2026-09-06 - Check for Updates / self-update, repo made public

### Added
- **Settings > About > Check for Updates**: queries this project's GitHub Releases
  (`internal/update.FetchLatest`, `GET /repos/keteague/PDT/releases/latest`) and compares the
  release's tag against `AppVersion` (`driver.CompareVersions` - already a generic dot-separated
  numeric comparator despite living in the driver package, reused here rather than duplicated).
  Reports one of: an error (most commonly "no releases have been published yet", since none exist
  yet - confirmed live against the real, now-public repo), "you are running the latest version", or a
  newer version available with an **Update Now** button.
- **Update Now** (`App.ApplyUpdate`): downloads the newer release's `PDT.exe` asset and installs it in
  place of the running executable, then relaunches it and quits - see "Self-update" below for how that
  works with no separate installer.
- `internal/update`: the above as a standalone, unit-tested package (`FetchLatest`, `Download`,
  `Apply`, `CleanupOldExe`) independent of Wails/`app.go` plumbing.

### Changed
- The GitHub repo (`keteague/PDT`) is now public - confirmed nothing sensitive is actually tracked in
  it first: the only driver-related files under git are small `.INF` text fixtures used by unit tests
  (largest ~320KB), never the real `Drivers/` package tree (which was never committed to begin with -
  it's resolved next to the running exe at runtime, per "Drivers folder layout" in the README).

### Verified
- `go build`/`go vet`/`go test` (including new `internal/update` tests against an `httptest` server)
  and `wails build` all clean.
- **The core mechanism this whole feature depends on** - replacing a *running* Windows .exe's own
  file with no separate installer or helper process - confirmed directly against a real running copy
  of `PDT.exe` before writing any of the surrounding code: `Rename-Item` on the running exe's file
  succeeded while it kept running, a new file could then be written back at the original path, and the
  running (now-renamed-underneath-it) process was confirmed still alive and fully functional
  throughout. This is the Windows behavior `internal/update.Apply` relies on (rename the running exe
  aside, move the downloaded one into its place).
- Launching a `requireAdministrator`-manifested child process (`exec.Command(exePath).Start()`, used
  to relaunch after an update) from an already-elevated parent was separately confirmed not to trigger
  a second UAC prompt, incidentally, while testing the above (`Start-Process` launched `PDT.exe`
  immediately with no `-Verb RunAs` and no prompt/delay from an already-elevated shell).
- Live end-to-end through the real UI (Settings > About > Check for Updates) against the real,
  now-public GitHub API: correctly reported "no releases have been published yet" (accurate - none
  exist yet). The full download-and-apply path is exercised by `internal/update`'s own tests against a
  local `httptest` server; the live "Update Now" path itself needs an actual GitHub release to test
  against, which doesn't exist yet.

### Release process (new requirement for Check for Updates to find anything)
- Bump `AppVersion` (`version.go`) and `wails.json`'s `info.productVersion` together, `wails build`,
  then create a GitHub Release tagged `v<AppVersion>` (e.g. `v0.1.1`) with `build/bin/PDT.exe` uploaded
  as a release asset named exactly `PDT.exe` - `CheckForUpdate` looks for that exact asset name.

### Self-update mechanism (no installer)
`ApplyUpdate` downloads the new `PDT.exe` next to the running one, then calls `internal/update.Apply`:
rename the running exe to `PDT.exe.old`, move the downloaded file to the original `PDT.exe` name, then
relaunch it and quit. This works because Windows only needs a running executable's *name* freed to
place a new file there - it doesn't need the file itself closed, and the running process keeps
executing unaffected from its now-renamed-away file until it exits on its own a moment later. The
`.old` file is cleaned up the next time the app starts (`update.CleanupOldExe`, called from
`startup()`), once whatever process left it behind is guaranteed to have already exited. No installer,
no separate updater binary, and no extra UAC prompt (the relaunch inherits this already-elevated
process's token) - deliberately simpler than adding an NSIS/Inno installer pipeline, which would only
be worth it if this app ever needed Start Menu shortcuts or an uninstaller entry, neither of which it
does today (a single portable exe next to a `Drivers/` folder).

## 2026-09-06 - External Sites tab spacing, About tab verification

### Fixed
- **Real bug, reported from live use**: in Settings > External Sites, each manufacturer's field had
  its own internal 6px label-to-input gap (`.modal-field`'s own `gap`), but `#settingsSitesPanel`
  itself was a plain `<div>` with no gap between the `.modal-field` labels/entries - so one entry's
  input sat flush against the next entry's label, reading as though that label belonged to the field
  above it. Fixed by giving `#settingsSitesPanel` its own `display: flex; flex-direction: column; gap:
  14px` - wider than the 6px internal gap, so each entry still reads as its own group. Confirmed via
  screenshot after rebuilding: consistent spacing across all five manufacturer entries.

### Verified
- Found a reliable way to drive the Settings modal after two earlier automation attempts (coordinate
  clicks + Tab navigation) landed on the wrong control: WebView2 doesn't populate its UI Automation
  tree with named/typed elements until something first requests it, and the gear button's accessible
  name is its glyph (`⚙`), not "Settings" - locating and invoking elements by their actual accessible
  name via `System.Windows.Automation`'s `InvokePattern`, rather than simulated clicks/keystrokes at
  believed coordinates, opened the modal and switched tabs reliably.
- Using that method, the previously-unconfirmed **Settings > About** tab is now screenshot-confirmed
  correct: name, version, author, and the GitHub link all render as expected.

## 2026-09-05 - App icon, titlebar name/version, Settings > About

### Added
- `version.go`: `AppVersion`/`appDisplayName`/`appAuthor`/`appRepoURL` constants - `AppVersion` must be
  kept manually in sync with `wails.json`'s `info.productVersion` (Go can't read that value back out
  at runtime; it only feeds the compiled exe's Win32 version resource and the manifest template).
  `main.go`'s window `Title` is now `fmt.Sprintf("%s v%s", appDisplayName, AppVersion)` -
  "Printer Deployment Tool v0.1.0" - instead of the bare "PDT".
- `cmd/geniconassets`: a permanent repo utility (not a one-off script) that procedurally draws the
  app's printer-glyph icon and writes `build/appicon.png` (1024x1024, the Linux/frontend-facing icon)
  and `build/windows/icon.ico` (16/24/32/48/64/128/256, PNG-compressed frames per the modern
  Vista+ ICO format - no external image asset or design tool involved). Renders at 4x supersample and
  box-filter-downsamples to each target size (averaged in premultiplied-alpha space); an
  un-anti-aliased first pass looked jagged and illegible at 16px, visually confirmed fixed by
  comparing upscaled previews of both versions before finalizing. Re-run manually
  (`go run ./cmd/geniconassets`) if the icon design ever needs to change - it's not part of the normal
  build.
- **Settings > About** tab: version, author, and a clickable GitHub link (`GetAppInfo`,
  `OpenRepoURL` -> `runtime.BrowserOpenURL`), sourced from the same `version.go` constants as the
  titlebar.

### Verified
- Titlebar text confirmed via screenshot (`PrintWindow`) showing "Printer Deployment Tool v0.1.0"
  alongside the new icon.
- The compiled `PDT.exe`'s Explorer-associated icon (`[System.Drawing.Icon]::ExtractAssociatedIcon()`)
  confirmed screenshot-matching the new design.
- `go build`/`go vet`/`go test` and `wails build` all clean.

### Not verified at the time (see the 2026-09-06 entry above)
- The Settings > About tab's actual rendered content wasn't confirmed interactively this round -
  simulated keyboard navigation to reach the Settings gear button landed on an unrelated field
  instead. Screenshot-confirmed correct the next day, once UI Automation's `InvokePattern` replaced
  coordinate-based clicks/keystrokes as the way to drive the Settings modal.

## 2026-09-05 - Yellow titlebar while deploying

### Added
- `titlebar_windows.go`: the titlebar turns yellow for the duration of a `Deploy` run and always
  resets to the OS default when done, including on an unexpected error (`defer`) - ports
  `Create-Printers.ps1`'s own `DwmHelper` (`DwmSetWindowAttribute`/`DWMWA_CAPTION_COLOR`, Windows 11
  22000+ only; a harmless no-op on older Windows, same as that original tool's note). The one new
  piece: Wails doesn't expose the native window handle through its public runtime API the way the
  original tool already had `$form.Handle` in hand, so `findMainWindow` locates it the same way an
  external tool would - `EnumWindows` filtered to this process's own PID, then to the one visible
  top-level window (a real process was confirmed to also have several invisible helper windows -
  `GDI+ Window`, `MSCTFIME UI`, two `Default IME` - that the visibility check correctly excludes).
  Found once and cached, since the handle never changes for the life of the process.

### Verified
- The `findMainWindow` matching logic (PID filter + visibility check) replicated directly in
  PowerShell against a real running `PDT.exe`: exactly one visible window matched, titled "PDT", with
  the four invisible helper windows correctly excluded.
- `DwmSetWindowAttribute` called directly against that same real window handle - confirmed
  screenshot-visible yellow titlebar, then confirmed reset back to the default color - the exact
  sequence `Deploy` now runs automatically.

## 2026-09-05 - Request elevation on launch

### Added
- `build/windows/wails.exe.manifest`: `<trustInfo>` / `requestedExecutionLevel level="requireAdministrator"`.
  Every real operation this app performs (all printer/port work) already required Administrator, so
  it now requests elevation outright on every launch rather than starting unelevated and failing
  partway through a deploy - the same reasoning as `Create-Printers.ps1`'s own auto-elevation, but
  far simpler for a compiled exe: this is purely a manifest entry, Windows shows the UAC prompt
  automatically, with none of the re-exec-itself-as-admin trick a `.ps1` (which can't embed a
  manifest) needed. Unlike that original tool, there's no unelevated fallback mode - declining UAC
  means the process never starts at all, which is fine here since there's no genuinely useful
  degraded mode to fall back to. Confirmed embedded correctly by inspecting the compiled `PDT.exe`'s
  resources directly for the `requireAdministrator` string after rebuilding (an actual UAC prompt
  can't be produced/observed from an already-elevated session, since Windows only prompts when
  crossing from unelevated to elevated).

## 2026-09-05 - Print Defaults/Preferences fix, "Print spooled documents first", Save Configuration filename

### Fixed
- **Real bug, found from a live deployment report**: Canon UFR II printers deployed with Monochrome +
  1-sided checked were showing "Color (Auto)" and "Duplex, long edge" in their own Properties dialog.
  Root cause, confirmed against the real printer via three independent checks
  (`Get-PrintConfiguration`, .NET `System.Drawing.Printing.PrinterSettings`, and re-running
  `Set-PrintConfiguration` directly): raw DEVMODE (`SetDuplexAndColor`, this rewrite's only
  duplex/color mechanism until now) was already being set *correctly* - confirmed via
  `PrinterSettings`, the same mechanism the deploy sequence's own verification step uses - but a
  Canon UFR II printer's own Properties dialog and `Get-PrintConfiguration` read from a *separate*
  PrintTicket-based configuration store that DEVMODE alone never touches, and it was staying stale at
  whatever the driver's own install-time default was. Fixed by additionally calling
  `Set-PrintConfiguration` (`internal/printer/windows/printconfig_windows.go`,
  `SetPrintConfigurationViaShell`, shelling out to `powershell.exe`) after the existing DEVMODE step -
  confirmed by that same three-way check all agreeing afterward. Best-effort/`[WARN]`-only, same as
  the DEVMODE step it supplements, since DEVMODE alone already governs actual print behavior for most
  consumers.
- `PRINTER_ATTRIBUTE_DO_COMPLETE_FIRST` ("Print spooled documents first" on the Advanced tab) was
  researched and defined (`structs_windows.go`) during Phase 2 but never actually wired into
  anything - no deployed printer ever had it enabled. Added `SetPrintSpooledDocumentsFirst`
  (`apf_windows.go`, same `PRINTER_INFO_2.Attributes` mechanism as APF) and call it unconditionally
  (`enable: true`) for every row - this isn't a per-row checkbox like APF, just a fixed default every
  deployment should get. Verified via `Get-WmiObject Win32_Printer`'s `Attributes` bitmask against a
  real deployed printer.
- Save Configuration's suggested filename was always the generic `printers.json`, ignoring
  SalesChain ID entirely. `App.SaveConfiguration` now defaults to `<SalesChainID>.json` when
  SalesChain ID is set (already restricted to filesystem-safe characters by the frontend's own input
  validation), falling back to `printers.json` only when it's blank.

## 2026-09-05 - SalesChain ID rejection feedback (flash + beep)

### Added
- SalesChain ID now flashes red and plays a short synthesized beep on any keystroke that actually
  gets rejected - a disallowed character stripped, or a reserved device name (`PRN`, `COM1`, ...)
  reverted - never for an ordinary edit, since re-sanitizing an already-valid string is always a
  no-op. Detected by comparing the field's raw value against what `setSalesChainId` actually applied;
  they only ever differ when this keystroke was rejected. The beep is a couple of Web Audio nodes
  torn down right after (`playInvalidDing`), not an embedded audio asset, and fails silently if audio
  is unavailable rather than blocking the rejection feedback on it.

## 2026-09-05 - Check for Updates, Settings > External Sites, zip-packaged drivers

### Added
- **Check for Updates** button in the Defaults panel: opens the selected manufacturer's configured
  driver-download page in the system browser (`OpenManufacturerURL` -> `runtime.BrowserOpenURL`).
  Deliberately not automated version-checking - no vendor exposes an API for that, and scraping five
  different download portals individually would be fragile and high-maintenance; this just saves
  hunting down the URL each time.
- **Settings > External Sites** tab: one editable URL per manufacturer (seeded with the pages
  provided - Canon, HP, Kyocera, Ricoh, Sharp - via `defaultManufacturerURLs` in `settings.go`),
  persisted alongside Save File Base Path in the same `settings.json`. Each manufacturer's URL falls
  back to its own default independently if left blank, both on load and on save.
- `internal/driver/zip.go`: `.zip`-packaged driver downloads are now extracted automatically before
  each manufacturer folder is scanned - confirmed necessary against a real package (Sharp's UD3
  driver ships as `UD3_07_PCL6_2510a.zip`, previously invisible to `BuildCatalog` since it only ever
  looked for `.inf` files already sitting on disk). Extracts `Foo.zip` to a sibling `Foo/` folder
  once; leaves it alone (no re-extraction) if that folder already exists, however it got there.
  Zip-slip protected (rejects any entry that would extract outside the destination folder); a
  corrupt/unreadable zip is skipped (with its partial output cleaned up so a later run retries)
  rather than failing the whole catalog scan.

### Fixed
- Found while testing the above (the extra zip-scanning I/O made it consistently reproducible,
  though the underlying bug already existed): `App`'s catalog-dependent methods
  (`DefaultDriverFor`, `Models`, `DriverCandidates`, `GetCatalogStatus`, `Deploy`, `GetSettings`,
  `SaveSettings`, `OpenManufacturerURL`, `OpenConfiguration`, `SaveConfiguration`) could run before
  `startup()` finished populating `catalog`/`modelIndex`/`settings` - Wails does not block the
  frontend's own script from running until `OnStartup` returns, so a `BuildCatalog` scan slow enough
  to still be running when the frontend fires its first call (e.g. `DefaultDriverFor` on page load)
  would silently see nil/zero-value state and return an empty result with no error. Fixed with a
  `ready` channel every such method now blocks on (`<-a.ready`) before proceeding, closed once
  `startup()` completes - confirmed by reproducing the empty-Driver-field symptom, then confirming
  it's gone after the fix, both against the real Drivers folder.

### Verified
- `DefaultDriverNameFor` re-confirmed correct against the real catalog in isolation (ruling it out as
  the cause once the startup-race symptom appeared) before diagnosing and fixing the real bug above.
- New tests: `TestBuildCatalog_ExtractsAndScansZippedDriverPackage` and
  `TestBuildCatalog_DoesNotReExtractExistingFolder` (zip fixture built on the fly with `archive/zip`
  rather than committing a binary `.zip` to the repo).
- Screenshot-confirmed (`PrintWindow`) the Defaults panel's new button placement and that the
  Settings modal stays correctly hidden on load - proactively checked the new `.tab-panel` CSS for
  the same `display` vs. `[hidden]` specificity conflict already found and fixed on `.modal-backdrop`
  earlier, and guarded it the same way (`:not([hidden])`) before it could ship broken.

## 2026-09-05 - Real combobox for Model/Driver, Drivers/Windows/<version> layout support

### Added
- `setupCombobox()` (`main.js`): a small, self-contained dropdown component replacing the native
  `<input list=...><datalist>` attempt - shows every current candidate on focus, filters live as you
  type, click/Enter/arrow-keys to select. Wired up for the Defaults panel's Model/Driver fields and
  every grid row's Model/Driver fields.
- `internal/driver.BuildCatalog` now scans `driversRoot/Windows/<any version folder>/<Manufacturer>/...`
  (merging every version folder found) instead of assuming manufacturer folders sit directly under
  `driversRoot` - matching the real Drivers folder as it's being reorganized (Windows and macOS sides
  broken out, `Drivers/Windows/11/<Manufacturer>/...` for the Windows side). Falls back to the old
  flat layout when there's no `Windows` subfolder at all, so existing `internal/driver/testdata`
  fixtures and any pre-reorg `Drivers` folder keep working unmodified.
- `TestBuildCatalog_WindowsVersionNestedLayout`: new test against a dedicated nested fixture
  (`testdata_windows_layout/Windows/11/Canon/...`, a copy of the existing flat Canon fixture one
  level deeper) proving the new layout is actually found and scanned correctly.

### Changed
- Port name prefix's text field now shows "IP_" as a real placeholder (grayed hint, gone once you
  type) instead of a pre-filled default value - and, as before, stays disabled until the checkbox is
  checked.
- The Defaults panel's Driver field now expands to fill the remaining width of its row (was a fixed
  `size="40"`), via `flex: 1` on its wrapping label/combo rather than a fixed character count.

### Not done (explicitly out of scope for this round)
- The macOS side of the Drivers folder reorg (`Drivers/macOS/<Manufacturer>/<version>/...`, plus an
  `OpenPrinting` PPD fallback bucket) is not read by `BuildCatalog` at all yet, and there is still no
  macOS `Deployer`. Most of the real macOS packages on disk are `.dmg` images (frequently wrapping a
  nested `.dmg`, ultimately containing a `.pkg` installer) - extracting driver files/PPDs from those
  without a macOS host or a full GUI installer run is a real open question needing its own design
  pass, not something to guess at inside this change. See the README's "Drivers folder layout"
  section.

### Process note
- Mid-verification, an automated screenshot (`CopyFromScreen` at coordinates believed to be PDT's
  window) instead captured an unrelated terminal window's content that had ended up on top at that
  screen position - discarded immediately, not analyzed further. Switched all subsequent screenshots
  to `PrintWindow` (captures a specific window's own content directly, independent of focus/z-order),
  which avoids the failure mode entirely. Further interactive automation (simulated clicks/typing)
  was halted after a mouse click aimed at PDT's title bar landed on a different, unrelated window
  instead - the coordinate math proved unreliable in this multi-window desktop environment, and
  continuing risked interacting with unrelated windows rather than PDT.

## 2026-09-05 - Toolbar reorder, UseExistingPort fallback, DPI/resize robustness

### Changed
- **Open Configuration**/**Save Configuration** moved from the toolbar to the top bar, to the right
  of SalesChain ID. **Add Printer**/**Remove Selected** moved to the far left of the toolbar (ahead
  of New CSV/Import CSV).
- `UseExistingPort` checked with no matching port for the row's IP is no longer a fatal error - it
  now falls back to creating a new port instead (logged `[INFO]`), same as if the checkbox were
  unchecked. The row's real intent ("give me a working port for this IP") is still satisfiable, so
  failing the whole row over it was unnecessarily strict. Updated the matching tooltip, grid header,
  and README wording accordingly.
- Added `MinWidth`/`MinHeight` (700x520) to the main window - otherwise unconstrained and freely
  resizable, this floor just keeps it from being shrunk below a size the flex-based layout has
  actually been verified to hold up at with no clipping or overlapping controls.

### Verified
- Confirmed `build/windows/wails.exe.manifest` already declares `permonitorv2` DPI awareness, which
  WebView2 honors automatically - the window and its content scale correctly per-monitor with no
  changes needed there.
- Resized the running app down to 700x520 (well below the 1024x768 default) and confirmed via
  screenshot that the flex-wrap layout reflows cleanly (the Manufacturer/Model/Driver row wraps, the
  Port subsection's checkboxes wrap to a second line) with no clipped or overlapping controls -
  the practical stand-in for "things need more room" that DPI scaling or a smaller display would
  also produce.
- Re-verified `UseExistingPort`'s new fallback behavior end to end against the real spooler via
  `pdtdebug deployrow ... useexisting` with no matching port present: logs the fallback, creates the
  port, and the row deploys successfully instead of failing.

## 2026-09-05 - Defaults panel restructure, SalesChain ID validation, tooltips

### Changed
- Defaults panel restructured into one outer "Defaults (used by 'Add Printer')" box containing
  Manufacturer/Model/Driver, then three labeled subsections: **Port** (Subnet, Port name prefix +
  its text field, Use existing port, SNMP), **Print Defaults** (Monochrome, 1-sided), **Advanced**
  (Enable APF).
- SalesChain ID now restricts input to letters, digits, hyphen, and underscore as you type (covers
  every DOS/shell-reserved character - `\/:*?"<>|` on Windows, `/` and `:` on macOS - without needing
  to enumerate them), and separately rejects the Windows reserved device names (`CON`, `PRN`, `AUX`,
  `NUL`, `COM0`-`COM9`, `LPT0`-`LPT9`, case-insensitive, whole-string only) by refusing the keystroke
  that would complete one and reverting to whatever was there just before. A loaded configuration's
  SalesChain ID goes through the same check (resetting to empty if the saved value itself is a
  reserved name, since there's no "just before" to revert to for a freshly loaded value).
- Added a `title` tooltip to every interactive control in the app - toolbar buttons, Defaults panel
  fields, grid column headers, and every per-row input - explaining what it does or what it affects.

### Fixed
- Found during manual testing of the first tooltip pass: several tooltip strings contain a literal
  `"` (e.g. `"SalesChain: <id>"`), which - embedded directly into a double-quoted `title="..."`
  attribute - closed the attribute early and leaked the rest of the tooltip text onto the page as
  visible content. Fixed by routing every tooltip through a shared `tip()` helper that HTML-escapes
  it first; verified visually (rebuilt, screenshotted) that the leak is gone.

## 2026-09-05 - Phase 4: Wails frontend

### Added
- `app.go`: the full `App` API the frontend calls - `Manufacturers`/`Models`/`DriverCandidates` for
  the grid's dropdowns, `NewCsvTemplate`/`ImportCsv`/`OpenConfiguration`/`SaveConfiguration` (each
  wrapping a native OS file dialog), and `Deploy` (streams a `"deploy-progress"` event per row in
  addition to returning every result at the end). `confirm` implements `printer.Confirm` via a native
  `runtime.MessageDialog` Yes/No box - the Wails equivalent of the original tool's WinForms
  `MessageBox`. The driver catalog builds once at startup from a `Drivers/` folder resolved next to
  the running executable (falling back to `./Drivers` for `wails dev`).
- `frontend/src/main.js` (+ `app.css`): a plain HTML/CSS/vanilla-JS grid (no framework) replacing the
  Wails vanilla template - top bar, Defaults panel, toolbar, the row grid, and a live timestamped/
  level-tagged log panel, wired to the `App` API above.
- Every multi-signal or error-bearing `App` method returns a small DTO (`PathResult`, `ImportResult`,
  `OpenConfigResult`, `DeployRowResult`) rather than a second/third raw return value or a bare Go
  `error` - confirmed by reading Wails' own binding-dispatch code that it only supports `(T)` or
  `(T, error)` return shapes, and that a bare `error` has no exported fields to JSON-marshal a message
  from at all (it would cross the wire as `{}`).
- `internal/printer/batch.go`: `DeployAllWithProgress` (`DeployAll` now a thin wrapper over it) - runs
  the batch exactly as before but also invokes a callback after each row, for `Deploy` above to stream
  progress from.

### Design notes
- The grid never fully re-renders while a deploy is running: `onDeployProgress` correlates each
  incoming event to its row by submission order (`activeDeploy.nextIndex`) and toggles only that row's
  success/failure CSS class directly via a stable per-row `_id` - not the row's `Name` (never required
  to be unique) and not its position in `state.rows` (shifts as rows are added/removed). A full
  re-render on every progress event - a real bug caught before shipping - would otherwise destroy
  focus and in-progress edits in any other row while a multi-minute HP deploy is still running
  elsewhere in the grid.

### Verified
- `go build`/`go vet`/`go test` all pass; `npm run build` (Vite) and `wails build` both produce a
  working `PDT.exe`.
- Launched the built app against the real 3.1 GB `Drivers/` package tree (copied next to the exe):
  starts with no catalog-load warning, Manufacturer/Model/Driver dropdowns populate live, Add Printer
  adds a row, and the layout matches the design pass end to end (screenshot-verified via a real
  window capture + simulated clicks, not just "it compiles").

### Changed (layout, found during live manual testing of the first build)
- Added the missing **Subnet** default field (pre-fills a new row's IP when set, adding a trailing
  `.` if the user didn't type one - matches the original tool's own Subnet-default behavior), placed
  in the Defaults panel with **Port name prefix** immediately to its right (moved there from the top
  bar).
- Reordered the Defaults panel's checkboxes so **SNMP** sits immediately left of **Enable APF**.
- The Defaults panel's Manufacturer `<select>` now matches every text field's size/styling (it had no
  explicit sizing before and was noticeably narrower).
- **Monochrome** and **1-sided** are now checked by default in the Defaults panel.

## 2026-09-05 - Phase 3: Deploy orchestration

### Added
- `internal/printer/windows/deploy_windows.go`: `Deployer`, the real `printer.Deployer` implementation
  - the full per-row Deploy sequence (port -> driver -> printer object -> conditional NUL:-to-real-port
  rebind -> print config -> APF) wired on top of every Phase 2 binding.
- `internal/printer/batch.go`: `DeployAll` runs every row in order and always continues past a row
  that failed fatally - a bad printer object is fatal for that row alone, never for the run.
- `internal/printer/log.go`: `Logger` - every deploy log line gets a `2006-01-02 15:04:05` timestamp
  and an `[INFO]`/`[OK]`/`[WARN]`/`[ERR]` tag, per spec.
- Fatal/warning split, per spec: failing to create or update the printer object itself (port, driver
  install, `CreatePrinter`/`SetInfo2`, the NUL:-rebind) is fatal for the row; failing to apply
  duplex/color or advanced printing features afterward is only ever a warning, and both are always
  attempted regardless of whether the other succeeded.
- No `BindNulPort` checkbox (unlike the original tool): typing `NUL`/`NUL:` as a row's IP binds it to
  `NUL:` permanently; any row resolving to HP's Universal Print Driver family automatically gets the
  create-against-`NUL:`-then-rebind treatment with no user toggle (`printer.RequiresNulPortWorkaround`).
- Driver version handling ported from the original tool's fix for the same bug: a plain-name
  selection silently upgrades an older installed driver and never downgrades; an explicit version pin
  (a decorated `<name> (vVersion - date)` catalog label) is honored upgrade-or-downgrade but only
  after a confirmation dialog, since Windows shares one driver registration per name across every
  printer already using it.
- Existing-printer-object handling ported from the original tool: if a printer with the row's exact
  name already exists, its current driver/port/comment are diffed against what the row would set and
  a confirmation dialog lists exactly what would change (or no prompt at all if nothing would).
- `internal/printer/windows/driverinfo_windows.go`: `GetInstalledDriverVersion` - reads an installed
  driver's real date/version straight from
  `HKLM\SYSTEM\CurrentControlSet\Control\Print\Environments\<env>\Drivers\Version-3\<name>`
  (`DriverDate`/`DriverVersion` values). Verified against a real machine to hold the exact same
  version string a vendor `.inf`'s own `DriverVer=` line declares (Canon `3.50.0.0`, Kyocera
  `8.6.1022.0`, HP `61.360.1.26819`) - a cleaner and more reliable source than `Get-PrinterDriver`'s
  CIM-derived `Date`/`DriverVersion` properties, which the original tool found came back unusable
  (garbage version, blank date) no matter how the driver was installed. This sidesteps the original
  tool's own workaround entirely (reading the installed driver's staged `.inf` back off disk via its
  `InfPath` and reparsing `DriverVer=`) - not needed here since the registry values themselves proved
  reliable.
- `internal/printer/windows/portlookup_windows.go`: `FindTcpIpPortByHost` - finds an existing Standard
  TCP/IP port already targeting a host by reading every port's `HostName` value from
  `HKLM\SYSTEM\CurrentControlSet\Control\Print\Monitors\Standard TCP/IP Port\Ports\<name>` - the
  registry-level equivalent of the original tool's `Win32_TCPIPPrinterPort.HostAddress` WMI lookup.
  Used unconditionally (not just when `UseExistingPort` is checked) so a redeploy of the same row
  never creates a genuine duplicate port for a host that already has one - porting forward the
  original tool's own fix for that exact bug.
- `driver.CompareVersions`: exported wrapper around the existing internal numeric version comparison,
  for comparing an installed driver's registry-sourced version against a `ResolvedDriver.Version`
  from outside the `driver` package.
- `cmd/pdtdebug`: `driverversion`, `findport`, and `deployrow` commands to exercise the new pieces
  (and the whole Deploy sequence end to end) against this machine's real spooler and a real local
  driver catalog, auto-confirming every prompt and cleaning up after itself.

### Fixed
- Driver "already installed at the same version" comparison used exact string equality on the version
  field, while the "is it newer" comparison used numeric `driver.CompareVersions` - found during
  end-to-end testing against a real HP driver, where the registry's version string (`61.360.1.26819`)
  and the vendor `.inf`'s own (`61.360.01.26819`) differ only by a leading zero. Both comparisons now
  use `driver.CompareVersions`, so a numerically-identical version is never mistaken for "needs
  updating."

### Verified (against this machine's real print spooler and driver packages)
- Fresh printer creation (Canon) with an auto-created, unprefixed (bare-IP-named) Standard TCP/IP port.
- HP Universal Print Driver: automatic NUL:-first creation followed by a rebind to the real port,
  end to end.
- Plain-name driver auto-upgrade (Kyocera 8.6.1022.0 installed -> 8.7.422.0 resolved, upgraded with no
  prompt) and the known, non-fatal Kyocera color-switchback quirk logged as `[WARN]`.
- Idempotent redeploy of an unchanged row (existing port reused, no confirmation prompt, no changes
  applied) and a changed row (port change correctly diffed, confirmed, and applied).
- `UseExistingPort` checked with no matching port present: fails fatally for that row with a clear
  message, exactly as documented.
- An explicit older-version driver pin (Kyocera v8.7 installed, row pins v8.6): confirmation dialog
  shown, downgrade applied on confirmation - the original tool's own confirmed-and-fixed bug
  ("switching to a different driver doesn't prompt"), now proven on the Go rewrite from the start.

### Known limitations (carried forward from the original tool)
- Kyocera's driver has been observed not to reliably switch back from monochrome to color via
  DEVMODE (confirmed non-transient by retrying); logged as `[WARN]`, deployment still succeeds.
- Confirmed the HP Universal Print Driver's multi-minute creation delay against a live TCP/IP port
  is **not** an artifact of the original tool's PowerShell/PrintManagement cmdlet layer: a direct,
  isolated timing test (`cmd/pdtdebug timedcreate`) calling this rewrite's syscall-based
  `CreatePrinter` straight against a real TCP/IP port, with no NUL: workaround involved at all, still
  took 4m4.6s - essentially identical to the original tool's own ~4-minute observation. The
  automatic NUL:-first-then-rebind workaround (`RequiresNulPortWorkaround`) is therefore still
  necessary and is not a leftover-only-for-the-old-implementation workaround.

## Prior (Phases 1-2, undated - reconstructed from the existing codebase)

- **Phase 1** (`internal/config`, `internal/driver`, `internal/printer` types): CSV/JSON
  import-export (`internal/config`), the multi-version/multi-arch driver catalog and name/version
  resolution (`internal/driver`), and the platform-independent row/request/result types
  (`internal/printer`).
- **Phase 2** (`internal/printer/windows`): hand-written Win32 syscall bindings for printer
  create/read/update, the "Advanced printing features" toggle, `NUL:` port creation, duplex/color via
  DEVMODE, driver installation, and Standard TCP/IP port creation - see the README's "Windows
  bindings" table for the full list and the reasoning behind each one.
