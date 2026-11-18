package fsx

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// ErrLocked: LockWait gave up, the lock is held by someone else (another
// process, or another handle in this one: these locks are per handle).
var ErrLocked = errors.New("locked by someone else")

// LockWait takes the same exclusive lock as Lock, but waits at most wait for
// it (0: one try). Lock waits forever, which a click in the window must not.
func LockWait(path string, wait time.Duration) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	deadline := time.Now().Add(wait)
	for {
		ol := new(windows.Overlapped)
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(h, 0, 1, 0, ol)
				f.Close()
			}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_IO_PENDING) {
			f.Close()
			return nil, err
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrLocked
		}
		time.Sleep(25 * time.Millisecond)
	}
}
