package winui

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
)

// ClipboardText returns the text on the clipboard ("" if none). It is read
// only when the user presses a button that asks for it.
func ClipboardText() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for i := 0; i < 5; i++ { // another program may hold the clipboard for a moment
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			break
		}
		if i == 4 {
			return ""
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer procCloseClipboard.Call()
	const cfUnicodeText = 13
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)
	return windows.UTF16PtrToString((*uint16)(foreign(p)))
}

func foreign(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// SignInURL picks a Claude sign-in address out of copied text. A long address
// copied from a console may have been split over lines, so whitespace inside
// it is dropped. Only https addresses on Anthropic's own sign-in hosts count.
func SignInURL(text string) (string, bool) {
	i := strings.Index(text, "https://")
	if i < 0 {
		return "", false
	}
	u := strings.Join(strings.Fields(text[i:]), "")
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" {
		return "", false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "claude.ai", "claude.com", "www.claude.com", "platform.claude.com", "console.anthropic.com", "anthropic.com":
	default:
		return "", false
	}
	if !strings.Contains(parsed.Path, "oauth") {
		return "", false
	}
	return parsed.String(), true
}

// OpenPrivate opens url in an Edge InPrivate window, so a sign-in there does
// not touch the account the user's normal browser is signed in to. The window
// belongs to a throw-away Edge profile in tmpRoot: InPrivate windows of one
// profile share their sign-in while any is open, which would connect the
// account added just before. If policy turns InPrivate off, the throw-away
// profile alone keeps the sign-ins apart.
func OpenPrivate(u, tmpRoot string) error {
	edge := FindEdge()
	if edge == "" {
		return errors.New("Microsoft Edge was not found")
	}
	CleanSignInProfiles(tmpRoot)
	dir := filepath.Join(tmpRoot, "signin-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	args := []string{"--user-data-dir=" + dir, "--no-first-run", "--no-default-browser-check"}
	if !inPrivateDisabled() {
		args = append(args, "--inprivate")
	}
	LetOthersComeForward()
	return exec.Command(edge, append(args, u)...).Start()
}

// ShellWord returns the path exe as one word of a command line a shell splits
// (Claude Code runs BROWSER that way): its short form if it has spaces,
// quoted if it has no short form.
func ShellWord(exe string) string {
	if !strings.ContainsAny(exe, " \t") {
		return exe
	}
	if p, err := windows.UTF16PtrFromString(exe); err == nil {
		buf := make([]uint16, windows.MAX_LONG_PATH)
		if n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf))); err == nil && n > 0 && int(n) < len(buf) {
			if s := windows.UTF16ToString(buf[:n]); !strings.ContainsAny(s, " \t") {
				return s
			}
		}
	}
	return `"` + exe + `"`
}

// CleanSignInProfiles removes throw-away sign-in profiles left from earlier
// days (they may hold claude.ai cookies). Ones in use are skipped.
func CleanSignInProfiles(tmpRoot string) {
	m, _ := filepath.Glob(filepath.Join(tmpRoot, "signin-*"))
	for _, d := range m {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > time.Hour {
			_ = os.RemoveAll(d)
		}
	}
}

func inPrivateDisabled() bool {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, `SOFTWARE\Policies\Microsoft\Edge`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetIntegerValue("InPrivateModeAvailability")
		k.Close()
		if err == nil && v == 1 {
			return true
		}
	}
	return false
}
