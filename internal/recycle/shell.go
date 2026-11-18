package recycle

import (
	"errors"
	"fmt"
	"io/fs"
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

// shellBin is the Windows Recycle Bin, reached through the shell's
// IFileOperation, the engine Explorer itself deletes with. Each item is its
// own operation, so the order holds and a failure stops the rest.
//
// What keeps an item from being deleted for good: preflight refuses every
// case in which the shell would delete instead of recycling (no bin on the
// drive, the bin turned off or set to delete at once, more than the bin
// holds, a path too long), and a progress sink cancels any delete the shell
// announces without its recycle flag. An item counts as moved only when the
// shell hands back the item it made in the bin.
type shellBin struct {
	facts facts
	// landed, when set, is told where each item went in the bin (tests).
	landed func(from, to string)
	// sid is the user's security id, which names their part of each
	// volume's bin; nil: this process's own (tests set another).
	sid func() (string, error)
}

var (
	ole32                           = windows.NewLazySystemDLL("ole32.dll")
	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procCoCreateInstance            = ole32.NewProc("CoCreateInstance")
	procSHCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	procSHQueryRecycleBinW          = shell32.NewProc("SHQueryRecycleBinW")

	clsidFileOperation = windows.GUID{Data1: 0x3AD05575, Data2: 0x8857, Data3: 0x4850, Data4: [8]byte{0x92, 0x77, 0x11, 0xB8, 0x5B, 0xDB, 0x8E, 0x09}}
	iidIFileOperation  = windows.GUID{Data1: 0x947AAB5F, Data2: 0x0A5C, Data3: 0x4C13, Data4: [8]byte{0xB4, 0xD6, 0x4B, 0xF7, 0x83, 0x6F, 0xC9, 0xF8}}
	iidProgressSink    = windows.GUID{Data1: 0x04B0F1A7, Data2: 0x9490, Data3: 0x44BC, Data4: [8]byte{0x96, 0xE1, 0x42, 0x96, 0xA3, 0x12, 0x52, 0xE2}}
	iidIShellItem      = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
	iidIUnknown        = windows.GUID{Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// Vtable slots (IUnknown 0-2), checked against the shell on Windows 11.
const (
	vtRelease = 2
	// IFileOperation
	vtAdvise                  = 3
	vtUnadvise                = 4
	vtSetOperationFlags       = 5
	vtDeleteItem              = 18
	vtPerformOperations       = 21
	vtGetAnyOperationsAborted = 22
	// IShellItem
	vtGetDisplayName = 5
)

const (
	fofAllowUndo        = 0x40
	fofNoUI             = 0x614 // FOF_SILENT | FOF_NOCONFIRMATION | FOF_NOERRORUI | FOF_NOCONFIRMMKDIR
	fofxRecycleOnDelete = 0x80000
	fofxEarlyFailure    = 0x100000

	tsfDeleteRecycleIfPossible = 0x80
	sigdnFileSysPath           = 0x80058000

	sOK          = 0
	eNoInterface = 0x80004002
	errCancelled = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)
)

func (b *shellBin) Preflight(paths []string) error { return preflight(paths, b.facts) }

// Holds looks through the user's part of the bin on path's volume,
// <volume>\$Recycle.Bin\<sid>: a recycled file is in it as $R<random> with
// the extension it had ($I<random> is the bin's note of where it came
// from). Only its names are read; nothing there is opened or moved.
func (b *shellBin) Holds(path string) int {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return 0
	}
	root, err := b.facts.root(path)
	if err != nil {
		return 0
	}
	sidOf := b.sid
	if sidOf == nil {
		sidOf = userSID
	}
	sid, err := sidOf()
	if err != nil {
		return 0
	}
	ext := filepath.Ext(path)
	return holding(filepath.Join(root, "$Recycle.Bin", sid), fi, func(name string) bool {
		return len(name) > 2 && strings.EqualFold(name[:2], "$R") && strings.EqualFold(filepath.Ext(name), ext)
	})
}

// userSID is this process's user, as S-1-5-21-....
func userSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

func (b *shellBin) Recycle(paths []string) (int, error) {
	for _, p := range paths {
		guard(p)
	}
	if err := b.Preflight(paths); err != nil {
		return 0, err
	}
	var moved int
	var err error
	if serr := onSTA(func() { moved, err = recycleEach(paths, b.move) }); serr != nil {
		return 0, fmt.Errorf("recycle: COM: %w", serr)
	}
	return moved, err
}

// onSTA runs f on a thread of its own in a single-threaded COM apartment,
// which IFileOperation needs. The goroutine ends still locked to the thread,
// so the thread ends with it and no other goroutine inherits its COM state.
// A thread some other code left in the multithreaded apartment is passed
// over for a new one.
func onSTA(f func()) error {
	const rpcEChangedMode = syscall.Errno(0x80010106)
	var err error
	for range 3 {
		done := make(chan error, 1)
		go func() {
			runtime.LockOSThread()
			err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
			if err != nil && err != syscall.Errno(1) { // S_FALSE: this thread was in one already
				done <- err
				return
			}
			defer func() { done <- nil }()
			defer windows.CoUninitialize()
			f()
		}()
		if err = <-done; err != rpcEChangedMode {
			return err
		}
	}
	return err
}

// comObject is a COM interface pointer: its first word points at the vtable.
type comObject struct {
	vtbl *[32]uintptr
}

// uintptrescapes keeps &x arguments alive and in place for the call.
//
//go:uintptrescapes
func comCall(obj *comObject, slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(obj.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

// foreign turns an address the shell gave us into a pointer.
func foreign(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// displayPath is a shell item's file system path, "" if it has none.
func displayPath(item uintptr) string {
	if item == 0 {
		return ""
	}
	var s *uint16
	if hr := comCall((*comObject)(foreign(item)), vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&s))); hr != sOK || s == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(s))
	return windows.UTF16PtrToString(s)
}

// move sends one item to the Recycle Bin. It runs on the STA thread.
func (b *shellBin) move(path string) error {
	var op *comObject
	if hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOperation)), uintptr(unsafe.Pointer(&op))); hr != sOK || op == nil {
		return fmt.Errorf("the shell's file operation is not there (%#x)", uint32(hr))
	}
	defer comCall(op, vtRelease)
	if hr := comCall(op, vtSetOperationFlags, fofAllowUndo|fofNoUI|fofxRecycleOnDelete|fofxEarlyFailure); hr != sOK {
		return fmt.Errorf("the shell refused the flags (%#x)", uint32(hr))
	}
	s := newSink(longPath(path))
	var cookie uint32
	if hr := comCall(op, vtAdvise, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&cookie))); hr != sOK {
		return fmt.Errorf("the shell refused the progress sink (%#x)", uint32(hr))
	}
	defer comCall(op, vtUnadvise, uintptr(cookie))
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var item *comObject
	if hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
		uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item))); hr != sOK || item == nil {
		return b.failed(path, uint32(hr))
	}
	defer comCall(item, vtRelease)
	if hr := comCall(op, vtDeleteItem, uintptr(unsafe.Pointer(item)), 0); hr != sOK {
		return b.failed(path, uint32(hr))
	}
	hr := uint32(comCall(op, vtPerformOperations))
	var aborted int32
	comCall(op, vtGetAnyOperationsAborted, uintptr(unsafe.Pointer(&aborted)))
	runtime.KeepAlive(s)

	res := s.result()
	if s.refused.Load() {
		root, _ := b.facts.root(path)
		return &Error{Drive: drive(root), Err: fmt.Errorf("%w: Windows would have deleted it for good", ErrUnavailable)}
	}
	if succeeded(hr) && aborted == 0 && res.posted && succeeded(res.hr) && res.landed != "" {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			if b.landed != nil {
				b.landed(path, res.landed)
			}
			return nil
		}
		return fmt.Errorf("it is still there after the Recycle Bin took it")
	}
	if res.posted && !succeeded(res.hr) {
		hr = res.hr
	}
	if succeeded(hr) {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			// The shell said nothing went wrong but made nothing in the bin.
			return fmt.Errorf("it left without the Recycle Bin saying it took it")
		}
		return fmt.Errorf("the shell left it in place")
	}
	return b.failed(path, hr)
}

// succeeded is SUCCEEDED(hr): success codes other than S_OK count too. A
// recycled file's PostDeleteItem says COPYENGINE_S_DONT_PROCESS_CHILDREN.
func succeeded(hr uint32) bool { return int32(hr) >= 0 }

// The shell's copy engine has HRESULTs of its own (sherrors.h, which is not
// on hand here). A file held open gives copyEngineSharingSrc, seen on
// Windows 11; the others are its neighbours in the header as remembered.
// They only choose the message: nothing is deleted on any of them.
const (
	copyEnginePathTooDeepSrc   = 0x8027001D
	copyEngineAccessDeniedSrc  = 0x80270021
	copyEnginePathNotFoundSrc  = 0x80270023
	copyEngineSharingSrc       = 0x80270027
	copyEngineRecycleForceNuke = 0x80270036
	copyEngineRecycleTooBig    = 0x80270037
	copyEngineRecyclePathLong  = 0x80270038
	copyEngineRecycleNoBin     = 0x8027003A
)

// failed sorts out a failing HRESULT.
func (b *shellBin) failed(path string, hr uint32) error {
	switch hr {
	case copyEngineSharingSrc, copyEngineAccessDeniedSrc:
		return ErrInUse
	case copyEnginePathTooDeepSrc, copyEngineRecyclePathLong:
		return ErrTooLong
	case copyEngineRecycleForceNuke, copyEngineRecycleTooBig, copyEngineRecycleNoBin:
		root, _ := b.facts.root(path)
		return &Error{Drive: drive(root), Err: fmt.Errorf("%w (%#x)", ErrUnavailable, hr)}
	case copyEnginePathNotFoundSrc:
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return errGone
		}
	}
	if hr&0xFFFF0000 == 0x80070000 {
		switch syscall.Errno(hr & 0xFFFF) {
		case windows.ERROR_SHARING_VIOLATION, windows.ERROR_LOCK_VIOLATION, windows.ERROR_ACCESS_DENIED:
			return ErrInUse
		case windows.ERROR_FILENAME_EXCED_RANGE:
			return ErrTooLong
		case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND:
			if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
				return errGone
			}
		}
	}
	return fmt.Errorf("Windows could not recycle it (%#x)", hr)
}

// ---- the progress sink ----------------------------------------------------------

// sink implements IFileOperationProgressSink. The shell calls PreDeleteItem
// before it deletes anything; without the recycle flag the delete would be
// for good, and the sink cancels it. PostDeleteItem hands over the item made
// in the bin: the proof the item went there.
type sink struct {
	vtbl    *[19]uintptr
	refs    int32
	want    string // the item, spelled out in full
	refused atomic.Bool

	mu    sync.Mutex
	posts []post
}

type post struct {
	path   string
	hr     uint32
	landed string // the item in the bin; "" if none
}

type sinkResult struct {
	posted bool
	hr     uint32
	landed string
}

func newSink(want string) *sink {
	return &sink{vtbl: sinkVtbl(), refs: 1, want: want}
}

// result is what the shell said about the item itself. Recycling a folder
// reports only the folder; anything else reported is a part of it.
func (s *sink) result() sinkResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.posts {
		if strings.EqualFold(p.path, s.want) {
			return sinkResult{posted: true, hr: p.hr, landed: p.landed}
		}
	}
	if len(s.posts) == 1 {
		p := s.posts[0]
		return sinkResult{posted: true, hr: p.hr, landed: p.landed}
	}
	return sinkResult{}
}

// The callbacks are made once, on first use: a Go program has room for only
// so many.
var sinkVtbl = sync.OnceValue(func() *[19]uintptr {
	var v [19]uintptr
	v[0] = syscall.NewCallback(func(this *sink, riid *windows.GUID, out *uintptr) uintptr {
		if *riid == iidIUnknown || *riid == iidProgressSink {
			atomic.AddInt32(&this.refs, 1)
			*out = uintptr(unsafe.Pointer(this))
			return sOK
		}
		*out = 0
		return eNoInterface
	})
	v[1] = syscall.NewCallback(func(this *sink) uintptr { return uintptr(atomic.AddInt32(&this.refs, 1)) })
	v[2] = syscall.NewCallback(func(this *sink) uintptr { return uintptr(atomic.AddInt32(&this.refs, -1)) })
	none := syscall.NewCallback(func(this *sink) uintptr { return sOK })
	one := syscall.NewCallback(func(this *sink, a uintptr) uintptr { return sOK })
	two := syscall.NewCallback(func(this *sink, a, b uintptr) uintptr { return sOK })
	three := syscall.NewCallback(func(this *sink, a, b, c uintptr) uintptr { return sOK })
	four := syscall.NewCallback(func(this *sink, a, b, c, d uintptr) uintptr { return sOK })
	five := syscall.NewCallback(func(this *sink, a, b, c, d, e uintptr) uintptr { return sOK })
	six := syscall.NewCallback(func(this *sink, a, b, c, d, e, f uintptr) uintptr { return sOK })
	seven := syscall.NewCallback(func(this *sink, a, b, c, d, e, f, g uintptr) uintptr { return sOK })
	v[3] = none  // StartOperations
	v[4] = one   // FinishOperations(hr)
	v[5] = three // PreRenameItem(flags, item, name)
	v[6] = five  // PostRenameItem(flags, item, name, hr, newItem)
	v[7] = four  // PreMoveItem(flags, item, dest, name)
	v[8] = six   // PostMoveItem(flags, item, dest, name, hr, newItem)
	v[9] = four  // PreCopyItem(flags, item, dest, name)
	v[10] = six  // PostCopyItem(flags, item, dest, name, hr, newItem)
	v[11] = syscall.NewCallback(func(this *sink, flags, item uintptr) uintptr {
		if uint32(flags)&tsfDeleteRecycleIfPossible == 0 {
			this.refused.Store(true)
			return errCancelled
		}
		return sOK
	})
	v[12] = syscall.NewCallback(func(this *sink, flags, item, hr, newItem uintptr) uintptr {
		p := post{path: displayPath(item), hr: uint32(hr)}
		if newItem != 0 {
			if p.landed = displayPath(newItem); p.landed == "" {
				p.landed = "?"
			}
		}
		this.mu.Lock()
		this.posts = append(this.posts, p)
		this.mu.Unlock()
		return sOK
	})
	v[13] = three // PreNewItem(flags, dest, name)
	v[14] = seven // PostNewItem(flags, dest, name, template, attrs, hr, newItem)
	v[15] = two   // UpdateProgress(total, soFar)
	v[16] = none  // ResetTimer
	v[17] = none  // PauseTimer
	v[18] = none  // ResumeTimer
	return &v
})

// ---- what Windows says about a volume --------------------------------------------

func osFacts() facts {
	return facts{
		root:      volumeRoot,
		fixed:     fixedDrive,
		hasBin:    hasRecycleBin,
		policyOff: recyclingOff,
		settings:  binSettings,
		scan:      scanTree,
	}
}

// volumeRoot is where the volume holding path is mounted: C:\, a folder a
// volume is mounted on, or \\server\share\.
func volumeRoot(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(p, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	root := windows.UTF16ToString(buf)
	if !strings.HasSuffix(root, `\`) {
		root += `\`
	}
	return root, nil
}

// fixedDrive: only a local fixed disk has a Recycle Bin to count on. A
// network drive has none, and a removable one usually none either.
func fixedDrive(root string) (bool, string) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, "a drive Windows doesn't know"
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_FIXED:
		return true, ""
	case windows.DRIVE_REMOTE:
		return false, "a network drive"
	case windows.DRIVE_REMOVABLE:
		return false, "a removable drive"
	case windows.DRIVE_CDROM:
		return false, "a CD or DVD drive"
	case windows.DRIVE_RAMDISK:
		return false, "a RAM disk"
	}
	return false, "a drive Windows doesn't know"
}

// hasRecycleBin asks the shell about the drive's Recycle Bin; it answers
// only for a drive that has one.
func hasRecycleBin(root string) bool {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	var info struct {
		cbSize   uint32
		size     int64
		numItems int64
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	hr, _, _ := procSHQueryRecycleBinW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&info)))
	return hr == sOK
}

// recyclingOff reads the policy "Do not move deleted files to the Recycle
// Bin", set for the user or for the machine.
func recyclingOff() bool {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, `Software\Microsoft\Windows\CurrentVersion\Policies\Explorer`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetIntegerValue("NoRecycleFiles")
		k.Close()
		if err == nil && v == 1 {
			return true
		}
	}
	return false
}

// binSettings reads the volume's Recycle Bin settings from Explorer: whether
// it deletes at once instead, and how much it holds (MaxCapacity, in MB). A
// volume Explorer has not set up yet has neither: the defaults are to
// recycle, with room for a share of the disk.
func binSettings(root string) (bool, int64, error) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, 0, err
	}
	buf := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(p, &buf[0], uint32(len(buf))); err != nil {
		return false, 0, err
	}
	name := windows.UTF16ToString(buf) // \\?\Volume{guid}\
	i, j := strings.IndexByte(name, '{'), strings.IndexByte(name, '}')
	if i < 0 || j < i {
		return false, 0, fmt.Errorf("volume name %q", name)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Explorer\BitBucket\Volume\`+name[i:j+1], registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	defer k.Close()
	nuke, _, err := k.GetIntegerValue("NukeOnDelete")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, 0, err
	}
	mb, _, err := k.GetIntegerValue("MaxCapacity")
	if err != nil {
		mb = 0
	}
	return nuke == 1, int64(mb) << 20, nil
}
