// Package webview2 shows a web page in a window of baton's own, using the
// Microsoft Edge WebView2 runtime that ships with Windows 11 (and almost all
// Windows 10). It needs no WebView2Loader.dll: it finds the installed runtime
// and calls its client DLL directly, and implements the few COM interfaces
// it needs in Go, with golang.org/x/sys only.
//
// Everything runs on the thread that calls Run, which must be locked
// (runtime.LockOSThread): COM objects, the window and its message loop.
package webview2

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procShowWindow          = user32.NewProc("ShowWindow")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procLoadIconW           = user32.NewProc("LoadIconW")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procSystemParametersW   = user32.NewProc("SystemParametersInfoW")
	procIsIconic            = user32.NewProc("IsIconic")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procCoTaskMemAlloc      = ole32.NewProc("CoTaskMemAlloc")
	procSetAppUserModelID   = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

// IIDs from WebView2.h.
var (
	iidIUnknown              = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidEnvironmentOptions    = windows.GUID{Data1: 0x2FDE08A8, Data2: 0x1E9A, Data3: 0x4766, Data4: [8]byte{0x8C, 0x05, 0x95, 0xA9, 0xCE, 0xB9, 0xD1, 0xC5}}
	iidEnvironmentCompleted  = windows.GUID{Data1: 0x4E8A3389, Data2: 0xC9D8, Data3: 0x4BD2, Data4: [8]byte{0xB6, 0xB5, 0x12, 0x4F, 0xEE, 0x6C, 0xC1, 0x4D}}
	iidControllerCompleted   = windows.GUID{Data1: 0x6C4819F3, Data2: 0xC9B7, Data3: 0x4260, Data4: [8]byte{0x81, 0x27, 0xC9, 0xF5, 0xBD, 0xE7, 0xF6, 0x8C}}
	iidNavigationStarting    = windows.GUID{Data1: 0x9ADBE429, Data2: 0xF36D, Data3: 0x432B, Data4: [8]byte{0x9D, 0xDC, 0xF8, 0x88, 0x1F, 0xBD, 0x76, 0xE3}}
	iidNewWindowRequested    = windows.GUID{Data1: 0xD4C185FE, Data2: 0xC81C, Data3: 0x4989, Data4: [8]byte{0x97, 0xAF, 0x2D, 0x3F, 0xA7, 0xAB, 0x56, 0x51}}
	stableRuntimeClientState = `Software\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
)

const (
	sOK          = 0
	eNoInterface = 0x80004002

	// vtable slots (IUnknown is 0-2)
	slotAddRef                = 1
	slotRelease               = 2
	envCreateController       = 3
	ctlPutIsVisible           = 4
	ctlPutBounds              = 6
	ctlMoveFocus              = 12
	ctlNotifyParentMoved      = 23 // NotifyParentWindowPositionChanged
	ctlClose                  = 24
	ctlGetCoreWebView2        = 25
	wvGetSettings             = 3
	wvNavigate                = 5
	wvAddNavigationStarting   = 7
	wvAddNewWindowRequested   = 44
	setPutIsStatusBarEnabled  = 10
	setPutAreDevToolsEnabled  = 12
	setPutAreHostObjectsAllow = 16
	navArgsGetURI             = 3
	navArgsPutCancel          = 8
	newWinArgsGetURI          = 3
	newWinArgsPutHandled      = 6

	wmDestroy      = 0x0002
	wmMove         = 0x0003
	wmSize         = 0x0005
	wmActivate     = 0x0006
	wmMoving       = 0x0216
	sizeMinimized  = 1
	wmClose        = 0x0010
	wmGetMinMax    = 0x0024
	wmDPIChanged   = 0x02E0
	wmApp          = 0x8000
	msgRestore     = wmApp + 1
	msgCall        = wmApp + 2
	swShow         = 5
	swRestore      = 9
	swpNoZOrder    = 0x0004
	swpNoActivate  = 0x0010
	spiGetWorkArea = 0x0030
)

// comPtr is an interface pointer owned by WebView2: its first word is the vtable.
type comPtr struct {
	vtbl *[128]uintptr
}

// uintptrescapes keeps &x arguments alive and in place for the call.
//
//go:uintptrescapes
func (p *comPtr) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(p.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(p))}, args...)...)
	return r
}

// ---- COM objects implemented here --------------------------------------------------

// handler is a COM callback object (IUnknown + Invoke with two arguments),
// the shape of every WebView2 completed and event handler.
type handler struct {
	vtbl   *[4]uintptr
	refs   int32
	iid    windows.GUID
	invoke func(a, b uintptr) uintptr
}

// pinned keeps objects WebView2 holds pointers to reachable for the GC.
var pinned sync.Map

var handlerVtbl = [4]uintptr{
	syscall.NewCallback(func(this *handler, riid *windows.GUID, out *uintptr) uintptr {
		if *riid == iidIUnknown || *riid == this.iid {
			atomic.AddInt32(&this.refs, 1)
			*out = uintptr(unsafe.Pointer(this))
			return sOK
		}
		if current != nil {
			current.logf("webview2: handler %s asked for %s", this.iid, *riid)
		}
		*out = 0
		return eNoInterface
	}),
	syscall.NewCallback(func(this *handler) uintptr { return uintptr(atomic.AddInt32(&this.refs, 1)) }),
	syscall.NewCallback(func(this *handler) uintptr { return uintptr(atomic.AddInt32(&this.refs, -1)) }),
	syscall.NewCallback(func(this *handler, a, b uintptr) uintptr { return this.invoke(a, b) }),
}

func newHandler(iid windows.GUID, invoke func(a, b uintptr) uintptr) *handler {
	h := &handler{vtbl: &handlerVtbl, refs: 1, iid: iid, invoke: invoke}
	pinned.Store(h, true)
	return h
}

// envOptions implements ICoreWebView2EnvironmentOptions with default values.
type envOptions struct {
	vtbl *[11]uintptr
	refs int32
}

func coString(s string) uintptr {
	u, _ := windows.UTF16FromString(s)
	p, _, _ := procCoTaskMemAlloc.Call(uintptr(len(u) * 2))
	if p == 0 {
		return 0
	}
	copy(unsafe.Slice((*uint16)(foreign(p)), len(u)), u)
	return p
}

// foreign turns an address that WebView2 or the OS gave us into a pointer.
func foreign(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

var envOptionsVtbl = func() [11]uintptr {
	str := func(s string) uintptr {
		return syscall.NewCallback(func(this *envOptions, out *uintptr) uintptr { *out = coString(s); return sOK })
	}
	ignore := syscall.NewCallback(func(this *envOptions, v uintptr) uintptr { return sOK })
	return [11]uintptr{
		syscall.NewCallback(func(this *envOptions, riid *windows.GUID, out *uintptr) uintptr {
			if *riid == iidIUnknown || *riid == iidEnvironmentOptions {
				atomic.AddInt32(&this.refs, 1)
				*out = uintptr(unsafe.Pointer(this))
				return sOK
			}
			if current != nil {
				current.logf("webview2: options asked for %s", *riid)
			}
			*out = 0
			return eNoInterface
		}),
		syscall.NewCallback(func(this *envOptions) uintptr { return uintptr(atomic.AddInt32(&this.refs, 1)) }),
		syscall.NewCallback(func(this *envOptions) uintptr { return uintptr(atomic.AddInt32(&this.refs, -1)) }),
		str(""), ignore, // AdditionalBrowserArguments
		str(""), ignore, // Language
		str("86.0.616.0"), ignore, // TargetCompatibleBrowserVersion: the first stable runtime
		syscall.NewCallback(func(this *envOptions, out *int32) uintptr { *out = 0; return sOK }), ignore, // AllowSingleSignOnUsingOSPrimaryAccount
	}
}()

// ---- runtime --------------------------------------------------------------------------

// clientDLL finds the installed WebView2 runtime's EmbeddedBrowserWebView.dll.
func clientDLL() (string, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64", "386": "x86"}[runtime.GOARCH]
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, stableRuntimeClientState, registry.QUERY_VALUE|registry.WOW64_32KEY)
		if err != nil {
			continue
		}
		dir, _, err := k.GetStringValue("EBWebView")
		k.Close()
		if err != nil || dir == "" {
			continue
		}
		p := filepath.Join(dir, "EBWebView", arch, "EmbeddedBrowserWebView.dll")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("the Microsoft Edge WebView2 runtime is not installed")
}

// Available reports whether a window can be shown (the runtime is installed).
func Available() bool {
	_, err := clientDLL()
	return err == nil
}

// SetAppID gives the process its own taskbar identity (and pin).
func SetAppID(id string) {
	if p, err := windows.UTF16PtrFromString(id); err == nil && procSetAppUserModelID.Find() == nil {
		_, _, _ = procSetAppUserModelID.Call(uintptr(unsafe.Pointer(p)))
	}
}

// ---- window ---------------------------------------------------------------------------

type Options struct {
	Title       string
	URL         string
	UserDataDir string
	Width       int    // in device-independent pixels
	Height      int    //
	Origin      string // pages outside it open in the default browser instead
	OpenOutside func(url string)
	IconID      uintptr // resource id of the app icon, 0 for none
	Log         func(format string, a ...any)
}

func (w *Window) logf(format string, a ...any) {
	if w.opt.Log != nil {
		w.opt.Log(format, a...)
	}
}

// Window is the one window Run shows.
type Window struct {
	opt        Options
	hwnd       uintptr
	controller *comPtr
	webview    *comPtr
	calls      sync.Map // id -> func()
	nextCall   atomic.Int64
	createErr  error
}

var (
	current   *Window // the window the message loop serves (one per process)
	wndProcCB = syscall.NewCallback(wndProc)
)

// HWND is the window handle, for dialogs that must be modal to it.
func (w *Window) HWND() uintptr { return w.hwnd }

// Restore shows the window and brings it to the front (any goroutine).
func (w *Window) Restore() { procPostMessageW.Call(w.hwnd, msgRestore, 0, 0) }

// Call runs fn on the window's thread and waits for it (any goroutine). It
// is how native dialogs are shown modal to the window.
func (w *Window) Call(fn func()) {
	id := w.nextCall.Add(1)
	w.calls.Store(id, fn)
	procSendMessageW.Call(w.hwnd, msgCall, uintptr(id), 0)
}

// Close closes the window (any goroutine).
func (w *Window) Close() { procPostMessageW.Call(w.hwnd, wmClose, 0, 0) }

// Run shows the window and returns when it has been closed. If WebView2
// cannot start, it returns an error before any window appears. onReady runs
// on the window thread once the page is loading.
func Run(opt Options, onReady func(*Window)) error {
	dllPath, err := clientDLL()
	if err != nil {
		return err
	}
	dll, err := windows.LoadDLL(dllPath)
	if err != nil {
		return fmt.Errorf("WebView2: %w", err)
	}
	create, err := dll.FindProc("CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return fmt.Errorf("WebView2: %w", err)
	}
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, windows.Errno(1)) {
		return fmt.Errorf("COM: %w", err)
	}

	w := &Window{opt: opt}
	current = w
	if err := w.createWindow(); err != nil {
		return err
	}
	if err := os.MkdirAll(opt.UserDataDir, 0o700); err != nil {
		return err
	}
	dataDir, _ := windows.UTF16PtrFromString(opt.UserDataDir)
	options := &envOptions{vtbl: &envOptionsVtbl, refs: 1}
	pinned.Store(options, true)

	envDone := newHandler(iidEnvironmentCompleted, func(hr, env uintptr) uintptr {
		w.logf("webview2: environment hr=0x%08x", uint32(hr))
		if uint32(hr) != 0 || env == 0 {
			w.fail(fmt.Errorf("WebView2 environment: 0x%08x", uint32(hr)))
			return sOK
		}
		e := (*comPtr)(foreign(env))
		ctlDone := newHandler(iidControllerCompleted, func(hr, ctl uintptr) uintptr {
			w.logf("webview2: controller hr=0x%08x", uint32(hr))
			if uint32(hr) != 0 || ctl == 0 {
				w.fail(fmt.Errorf("WebView2 controller: 0x%08x", uint32(hr)))
				return sOK
			}
			w.controller = (*comPtr)(foreign(ctl))
			w.controller.call(slotAddRef)
			var wv *comPtr
			w.controller.call(ctlGetCoreWebView2, uintptr(unsafe.Pointer(&wv)))
			if wv == nil {
				w.fail(errors.New("WebView2: no webview"))
				return sOK
			}
			w.webview = wv
			w.setup()
			w.resize()
			w.controller.call(ctlPutIsVisible, 1)
			procShowWindow.Call(w.hwnd, swShow)
			procSetForegroundWindow.Call(w.hwnd)
			u, _ := windows.UTF16PtrFromString(opt.URL)
			w.webview.call(wvNavigate, uintptr(unsafe.Pointer(u)))
			w.controller.call(ctlMoveFocus, 0)
			if onReady != nil {
				onReady(w)
			}
			return sOK
		})
		if hr := e.call(envCreateController, w.hwnd, uintptr(unsafe.Pointer(ctlDone))); hr != 0 {
			w.fail(fmt.Errorf("WebView2 controller: 0x%08x", uint32(hr)))
		}
		return sOK
	})
	hr, _, _ := create.Call(1, 0, uintptr(unsafe.Pointer(dataDir)), uintptr(unsafe.Pointer(options)), uintptr(unsafe.Pointer(envDone)))
	w.logf("webview2: %s create hr=0x%08x", dllPath, uint32(hr))
	if hr != 0 {
		procDestroyWindow.Call(w.hwnd)
		return fmt.Errorf("WebView2: 0x%08x", uint32(hr))
	}
	runtime.KeepAlive(dataDir)

	var msg [48]byte // MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
	if w.controller != nil {
		w.controller.call(ctlClose)
	}
	return w.createErr
}

// fail ends the window before it was ever shown, so the caller can fall back.
func (w *Window) fail(err error) {
	w.createErr = err
	procDestroyWindow.Call(w.hwnd)
}

func (w *Window) createWindow() error {
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cls, _ := windows.UTF16PtrFromString("baton.window")
	title, _ := windows.UTF16PtrFromString(w.opt.Title)
	icon := uintptr(0)
	if w.opt.IconID != 0 {
		icon, _, _ = procLoadIconW.Call(hinst, w.opt.IconID)
	}
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, 32512) // IDI_APPLICATION
	}
	cursor, _, _ := procLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := struct {
		cbSize        uint32
		style         uint32
		lpfnWndProc   uintptr
		cbClsExtra    int32
		cbWndExtra    int32
		hInstance     uintptr
		hIcon         uintptr
		hCursor       uintptr
		hbrBackground uintptr
		lpszMenuName  *uint16
		lpszClassName *uint16
		hIconSm       uintptr
	}{lpfnWndProc: wndProcCB, hInstance: hinst, hIcon: icon, hCursor: cursor, hbrBackground: 6, lpszClassName: cls, hIconSm: icon}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = 96
	}
	width, height := scale(w.opt.Width, dpi), scale(w.opt.Height, dpi)
	var work struct{ left, top, right, bottom int32 }
	procSystemParametersW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	if ww := int(work.right - work.left); ww > 0 && width > ww {
		width = ww
	}
	if wh := int(work.bottom - work.top); wh > 0 && height > wh {
		height = wh
	}
	x := int(work.left) + (int(work.right-work.left)-width)/2
	y := int(work.top) + (int(work.bottom-work.top)-height)/2
	const wsOverlappedWindow = 0x00CF0000
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("create window: %w", err)
	}
	w.hwnd = hwnd
	return nil
}

func scale(dip int, dpi uintptr) int { return dip * int(dpi) / 96 }

// setup applies the settings and the navigation rules.
func (w *Window) setup() {
	var settings *comPtr
	w.webview.call(wvGetSettings, uintptr(unsafe.Pointer(&settings)))
	if settings != nil {
		settings.call(setPutAreDevToolsEnabled, 0)
		settings.call(setPutIsStatusBarEnabled, 0)
		settings.call(setPutAreHostObjectsAllow, 0)
		settings.call(slotRelease)
	}
	var token int64
	// Only baton's own pages load in the window; any other address opens in
	// the user's browser.
	nav := newHandler(iidNavigationStarting, func(sender, args uintptr) uintptr {
		a := (*comPtr)(foreign(args))
		var uri *uint16
		a.call(navArgsGetURI, uintptr(unsafe.Pointer(&uri)))
		u := takeString(uri)
		if !w.inside(u) {
			a.call(navArgsPutCancel, 1)
			w.outside(u)
		}
		return sOK
	})
	w.webview.call(wvAddNavigationStarting, uintptr(unsafe.Pointer(nav)), uintptr(unsafe.Pointer(&token)))
	newWin := newHandler(iidNewWindowRequested, func(sender, args uintptr) uintptr {
		a := (*comPtr)(foreign(args))
		var uri *uint16
		a.call(newWinArgsGetURI, uintptr(unsafe.Pointer(&uri)))
		a.call(newWinArgsPutHandled, 1)
		w.outside(takeString(uri))
		return sOK
	})
	w.webview.call(wvAddNewWindowRequested, uintptr(unsafe.Pointer(newWin)), uintptr(unsafe.Pointer(&token)))
}

func (w *Window) inside(u string) bool {
	o := w.opt.Origin
	return o != "" && (u == o || strings.HasPrefix(u, o+"/") || strings.HasPrefix(u, o+"?"))
}

func (w *Window) outside(u string) {
	if w.opt.OpenOutside != nil && (strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")) {
		w.opt.OpenOutside(u)
	}
}

func takeString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}

func (w *Window) resize() {
	if w.controller == nil {
		return
	}
	var r struct{ left, top, right, bottom int32 }
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	if runtime.GOARCH == "arm64" {
		// A 16-byte struct passed by value travels in two registers on arm64.
		lo := uintptr(uint32(r.left)) | uintptr(uint32(r.top))<<32
		hi := uintptr(uint32(r.right)) | uintptr(uint32(r.bottom))<<32
		w.controller.call(ctlPutBounds, lo, hi)
		return
	}
	w.controller.call(ctlPutBounds, uintptr(unsafe.Pointer(&r)))
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	w := current
	if w == nil || hwnd != w.hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
		return r
	}
	switch msg {
	case wmSize:
		if w.controller != nil {
			// The page cannot tell it is minimized unless told: it then
			// polls less often (document.visibilityState).
			visible := uintptr(1)
			if wparam == sizeMinimized {
				visible = 0
			}
			w.controller.call(ctlPutIsVisible, visible)
		}
		w.resize()
		return 0
	case wmMove, wmMoving:
		// The browser's window is in another process and does not see its
		// parent move: without this, dropdowns open where the window was.
		if w.controller != nil {
			w.controller.call(ctlNotifyParentMoved)
		}
	case wmActivate:
		if wparam&0xffff != 0 && w.controller != nil {
			w.controller.call(ctlMoveFocus, 0)
		}
	case wmGetMinMax:
		dpi, _, _ := procGetDpiForWindow.Call(hwnd)
		if dpi == 0 {
			dpi = 96
		}
		mmi := (*[10]int32)(foreign(lparam)) // MINMAXINFO: ptMinTrackSize at index 6,7
		mmi[6], mmi[7] = int32(scale(720, dpi)), int32(scale(520, dpi))
		return 0
	case wmDPIChanged:
		r := (*[4]int32)(foreign(lparam))
		procSetWindowPos.Call(hwnd, 0, uintptr(r[0]), uintptr(r[1]), uintptr(r[2]-r[0]), uintptr(r[3]-r[1]), swpNoZOrder|swpNoActivate)
		return 0
	case msgRestore:
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
			procShowWindow.Call(hwnd, swRestore)
		}
		procShowWindow.Call(hwnd, swShow)
		procSetForegroundWindow.Call(hwnd)
		return 0
	case msgCall:
		if fn, ok := w.calls.LoadAndDelete(int64(wparam)); ok {
			fn.(func())()
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}
