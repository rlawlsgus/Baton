package winui

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                    = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance     = ole32.NewProc("CoCreateInstance")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput    = user32.NewProc("AttachThreadInput")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	procGetCurrentThreadId   = kernel32.NewProc("GetCurrentThreadId")
	clsidFileOpenDialog      = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog       = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	errCancelled             = uintptr(0x800704C7) // HRESULT_FROM_WIN32(ERROR_CANCELLED)
	errFolderPickerFailed    = errors.New("the folder dialog could not be shown")
	folderDialogRequests     = make(chan folderRequest)
	folderDialogThreadLocked = make(chan struct{}, 1)
)

// comObject is a COM interface pointer: its first word points at the vtable.
type comObject struct {
	vtbl *[32]uintptr
}

// IFileDialog vtable slots used here (IUnknown 0-2, IModalWindow 3, IFileDialog 4-26).
const (
	vtRelease        = 2
	vtShow           = 3
	vtSetOptions     = 9
	vtGetOptions     = 10
	vtSetTitle       = 17
	vtGetResult      = 20
	vtGetDisplayName = 5 // IShellItem
)

const (
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosPathMustExist   = 0x800
	sigdnFileSysPath   = 0x80058000
)

type folderRequest struct {
	title string
	reply chan folderReply
}

type folderReply struct {
	path string
	err  error
}

// PickFolderOwned shows the folder dialog modal to owner. It must run on
// owner's thread, which has COM initialized (the GUI window's thread).
func PickFolderOwned(owner uintptr, title string) (string, error) {
	return showFolderDialog(owner, title)
}

// PickFolder shows the Windows folder dialog in front of other windows (when
// there is no window of baton's own to own it) and returns the chosen
// folder, or "" if the user cancelled.
func PickFolder(title string) (string, error) {
	startFolderThread()
	req := folderRequest{title: title, reply: make(chan folderReply, 1)}
	folderDialogRequests <- req
	r := <-req.reply
	return r.path, r.err
}

// The dialog is a COM object that must live on one single-threaded-apartment
// thread, so one locked goroutine serves every request.
func startFolderThread() {
	select {
	case folderDialogThreadLocked <- struct{}{}:
	default:
		return
	}
	go func() {
		runtime.LockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil {
			for req := range folderDialogRequests {
				req.reply <- folderReply{err: err}
			}
			return
		}
		for req := range folderDialogRequests {
			owner := frontOwner()
			p, err := showFolderDialog(owner, req.title)
			procDestroyWindow.Call(owner)
			req.reply <- folderReply{path: p, err: err}
		}
	}()
}

// uintptrescapes keeps &x arguments alive and in place for the call.
//
//go:uintptrescapes
func comCall(obj *comObject, slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(obj.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

func showFolderDialog(owner uintptr, title string) (string, error) {
	var dlg *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg))); hr != 0 || dlg == nil {
		return "", errFolderPickerFailed
	}
	defer comCall(dlg, vtRelease)
	var opts uint32
	comCall(dlg, vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	comCall(dlg, vtSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem|fosPathMustExist))
	if t, err := windows.UTF16PtrFromString(title); err == nil && title != "" {
		comCall(dlg, vtSetTitle, uintptr(unsafe.Pointer(t)))
	}
	switch hr := comCall(dlg, vtShow, owner); {
	case hr == errCancelled:
		return "", nil
	case hr != 0:
		return "", errFolderPickerFailed
	}
	var item *comObject
	if hr := comCall(dlg, vtGetResult, uintptr(unsafe.Pointer(&item))); hr != 0 || item == nil {
		return "", errFolderPickerFailed
	}
	defer comCall(item, vtRelease)
	var name *uint16
	if hr := comCall(item, vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); hr != 0 || name == nil {
		return "", errFolderPickerFailed
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	return windows.UTF16PtrToString(name), nil
}

// frontOwner makes an invisible topmost window to own the dialog, so it opens
// above the GUI window even though baton is not the foreground process, and
// takes the foreground through the foreground thread's input queue.
func frontOwner() uintptr {
	const (
		wsExTopmost    = 0x00000008
		wsExToolWindow = 0x00000080
		wsPopup        = 0x80000000
		smCxScreen     = 0
		smCyScreen     = 1
	)
	cls, _ := windows.UTF16PtrFromString("STATIC")
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	hwnd, _, _ := procCreateWindowExW.Call(wsExTopmost|wsExToolWindow, uintptr(unsafe.Pointer(cls)), 0, wsPopup,
		cx/2, cy/3, 0, 0, 0, 0, 0, 0)
	if hwnd == 0 {
		return 0
	}
	fg, _, _ := procGetForegroundWindow.Call()
	if fg != 0 {
		fgThread, _, _ := procGetWindowThreadPID.Call(fg, 0)
		me, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && fgThread != me {
			procAttachThreadInput.Call(me, fgThread, 1)
			procSetForegroundWindow.Call(hwnd)
			procAttachThreadInput.Call(me, fgThread, 0)
		}
	}
	return hwnd
}
