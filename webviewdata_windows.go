package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// webviewDataNotice describes a WebView2 data-folder fallback, for startup to
// surface in the log once the UI exists ("" when the default folder worked).
var webviewDataNotice string

// webviewUserDataPath picks the WebView2 user data folder, verified writable
// up front.
//
// Why: PDT never set one, so go-webview2 used its own default,
// %AppData%\PDT.exe. At a client site (2026-11-02) PDT, elevated with the
// local admin's own credentials, couldn't write that admin's own
// %AppData%\PDT.exe\EBWebView. WebView2 then shows "Microsoft Edge can't read
// and write to its data directory" and PDT never opens, with nothing PDT can
// do about it after the fact. The root cause on that machine is not yet known
// (profile state, redirection or security software are all candidates), so
// this degrades through progressively more machine-level locations instead
// of betting on one. %ProgramData% is writable by any elevated admin
// regardless of the state of their own profile; PDT is requireAdministrator,
// so that's always true here.
//
// The first candidate is exactly go-webview2's old default, so existing
// installs keep their saved UI state. When it fails, the reasons are also
// written to %ProgramData%\PDT\webview2-startup.log, which survives even if
// every candidate fails and PDT never shows a window.
func webviewUserDataPath() string {
	var candidates []string
	shared := ""
	if pd := programDataPDT(); pd != "" {
		shared = filepath.Join(pd, "WebView2", currentUserKey())
	}
	// Running as a different account than the one signed in to this session
	// (UAC prompt answered with another admin's credentials) is exactly the
	// failing case: the same admin's profile works fine via Switch Account,
	// in their own session, but WebView2 can't use it from inside someone
	// else's session even though plain file writes there succeed - so the
	// writability probe alone can't catch it. Go straight to %ProgramData%.
	if other, sessUser := runningAsOtherSessionUser(); other && shared != "" {
		candidates = append(candidates, shared)
		writeStartupLog("running inside " + sessUser + "'s session as a different account; using " + shared)
	}
	if p := os.Getenv("APPDATA"); p != "" {
		candidates = append(candidates, filepath.Join(p, "PDT.exe"))
	}
	if p := os.Getenv("LOCALAPPDATA"); p != "" {
		candidates = append(candidates, filepath.Join(p, "PDT", "WebView2"))
	}
	if shared != "" {
		candidates = append(candidates, shared)
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "PDT-WebView2"))

	var failures []string
	for _, c := range candidates {
		if err := dirWritable(c); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", c, err))
			continue
		}
		if len(failures) > 0 {
			webviewDataNotice = "WebView2 data folder fell back to " + c + " - not writable: " + strings.Join(failures, "; ")
			writeStartupLog(webviewDataNotice)
		}
		return c
	}
	writeStartupLog("No writable WebView2 data folder - " + strings.Join(failures, "; "))
	return "" // let go-webview2 try its own default
}

// dirWritable creates dir if needed and proves a file can be written in it.
func dirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".pdt-write-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func programDataPDT() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT); err == nil && p != "" {
		return filepath.Join(p, "PDT")
	}
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "PDT")
	}
	return ""
}

// currentUserKey keeps each account's WebView2 state separate under the
// shared %ProgramData% folder: the token's SID, or the username if the SID
// can't be read.
func currentUserKey() string {
	if tu, err := windows.GetCurrentProcessToken().GetTokenUser(); err == nil {
		return tu.User.Sid.String()
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "default"
}

var (
	wtsapi32                     = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInfoW     = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory            = wtsapi32.NewProc("WTSFreeMemory")
	wtsCurrentServerHandle       = uintptr(0)
	wtsUserName              int = 5 // WTS_INFO_CLASS WTSUserName
	wtsDomainName            int = 7 // WTS_INFO_CLASS WTSDomainName
)

// wtsSessionString returns one WTSQuerySessionInformationW string field.
func wtsSessionString(session uint32, class int) (string, bool) {
	var buf *uint16
	var n uint32
	r, _, _ := procWTSQuerySessionInfoW.Call(wtsCurrentServerHandle, uintptr(session), uintptr(class),
		uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil {
		return "", false
	}
	defer procWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	return windows.UTF16PtrToString(buf), true
}

// runningAsOtherSessionUser reports whether this process's account differs
// from the account signed in to its Windows session, plus that session
// user's DOMAIN\name. False whenever either side can't be determined, so a
// lookup failure keeps the normal behavior.
func runningAsOtherSessionUser() (bool, string) {
	var session uint32
	if err := windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &session); err != nil {
		return false, ""
	}
	sessName, ok1 := wtsSessionString(session, wtsUserName)
	sessDomain, ok2 := wtsSessionString(session, wtsDomainName)
	if !ok1 || !ok2 || sessName == "" {
		return false, ""
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false, ""
	}
	name, domain, _, err := tu.User.Sid.LookupAccount("")
	if err != nil {
		return false, ""
	}
	sess := sessDomain + `\` + sessName
	return !strings.EqualFold(sess, domain+`\`+name), sess
}

func writeStartupLog(msg string) {
	pd := programDataPDT()
	if pd == "" || os.MkdirAll(pd, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(pd, "webview2-startup.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s PDT v%s user=%s %s\r\n", time.Now().Format(time.RFC3339), AppVersion, os.Getenv("USERNAME"), msg)
}
