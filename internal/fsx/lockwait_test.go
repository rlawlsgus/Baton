package fsx

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// LockWait gives up after its wait while the lock is held, also by another
// handle of this process, and takes it once it is free.
func TestLockWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := LockWait(path, 150*time.Millisecond); !errors.Is(err, ErrLocked) {
		t.Fatalf("held: %v", err)
	}
	if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
		t.Fatalf("waited %v", d)
	}
	if _, err := LockWait(path, 0); !errors.Is(err, ErrLocked) {
		t.Fatalf("one try: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		unlock()
	}()
	again, err := LockWait(path, 5*time.Second)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()
	if u, err := LockWait(path, 0); err != nil {
		t.Fatalf("free: %v", err)
	} else {
		u()
	}
}
