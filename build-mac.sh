#!/bin/sh
# build-mac.sh - `wails build`, then re-applies issue #9's own confirmed
# local-only workaround for the AMFI SIGKILL a plain ad-hoc-signed build
# hits the moment PDT tries to elevate privileges (Deploy's own auth
# prompt) - see https://github.com/keteague/PDT/issues/9 for the full root
# cause. Every plain `wails build` re-signs ad-hoc from scratch, silently
# undoing this workaround each time - use this script instead for any local
# macOS build meant to actually run a real (privileged) Deploy.
#
# The "PDT Local Dev" identity is a self-signed cert generated and trusted
# once, by hand, on a single machine (issue #9's own setup steps) - it does
# NOT generalize to any other endpoint. This script degrades to a plain,
# still-ad-hoc `wails build` (with a clear warning) on any machine that
# hasn't gone through that one-time setup.
set -e
cd "$(dirname "$0")"

WAILS_BIN=wails
if ! command -v "$WAILS_BIN" >/dev/null 2>&1; then
    WAILS_BIN="$(go env GOPATH 2>/dev/null)/bin/wails"
fi

"$WAILS_BIN" build "$@"

if security find-identity -v -p codesigning 2>/dev/null | grep -q '"PDT Local Dev"'; then
    codesign --force --deep --sign "PDT Local Dev" --options runtime build/bin/PDT.app
    echo "Re-signed build/bin/PDT.app with the local 'PDT Local Dev' identity (issue #9's own workaround) - privileged elevation will work on this machine."
else
    echo "No 'PDT Local Dev' signing identity found - build/bin/PDT.app is still ad-hoc signed."
    echo "Privileged elevation (Deploy) will crash on this machine (issue #9) until either a real Developer ID cert is used, or issue #9's own local workaround is set up here."
fi
