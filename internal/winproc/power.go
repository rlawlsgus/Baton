package winproc

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procPowerCreateRequest = kernel32.NewProc("PowerCreateRequest")
	procPowerSetRequest    = kernel32.NewProc("PowerSetRequest")
	procPowerClearRequest  = kernel32.NewProc("PowerClearRequest")
)

// Awake is a Windows power request: while it is on, the PC does not go to
// sleep by itself when idle. The screen can still turn off, and the user
// can still put the PC to sleep (Sleep, closing a laptop's lid). Windows
// lists it, with Reason, in `powercfg /requests`.
type Awake struct {
	Reason string
	h      windows.Handle
	on     bool
}

// On reports whether the request is on.
func (a *Awake) On() bool { return a.on }

// Set turns the request on or off.
func (a *Awake) Set(on bool) error {
	if on == a.on {
		return nil
	}
	if a.h == 0 {
		if !on {
			return nil
		}
		reason, err := windows.UTF16PtrFromString(a.Reason)
		if err != nil {
			return err
		}
		// REASON_CONTEXT with POWER_REQUEST_CONTEXT_SIMPLE_STRING.
		ctx := struct {
			Version uint32
			Flags   uint32
			Reason  *uint16
		}{0, 1, reason}
		h, _, err := procPowerCreateRequest.Call(uintptr(unsafe.Pointer(&ctx)))
		runtime.KeepAlive(reason)
		if h == 0 || windows.Handle(h) == windows.InvalidHandle {
			return err
		}
		a.h = windows.Handle(h)
	}
	const powerRequestSystemRequired = 1
	proc := procPowerClearRequest
	if on {
		proc = procPowerSetRequest
	}
	if ok, _, err := proc.Call(uintptr(a.h), powerRequestSystemRequired); ok == 0 {
		return err
	}
	a.on = on
	return nil
}

// Close turns the request off for good.
func (a *Awake) Close() {
	if a.h != 0 {
		_ = a.Set(false)
		windows.CloseHandle(a.h)
		a.h = 0
	}
}
