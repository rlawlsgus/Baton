package main

import (
	"os"
	"path/filepath"
)

// fakeOutsideWorld points what the test build must never reach for real at
// stand-ins, unless the tester named other folders already: the Recycle Bin
// is a folder, and the Claude desktop app is not there at all ("none") and
// is never running, so a delete never goes to the real bin, a switch of the
// app's sign-in never moves the real one's, and the window offers nothing of
// an app that is not there. The bin is a folder of baton's own (BATON_HOME),
// or of the temporary folder when that is not set. Every process baton
// starts inherits them.
func fakeOutsideWorld() {
	base := os.Getenv("BATON_HOME")
	if base != "" {
		if abs, err := filepath.Abs(base); err == nil {
			base = abs
		}
	} else {
		base = filepath.Join(os.TempDir(), "baton-test")
	}
	for _, kv := range [][2]string{
		{"BATON_RECYCLE", filepath.Join(base, "test-recycle-bin")},
		{"BATON_DESKTOP_DATA", "none"},
		{"BATON_DESKTOP_RUNNING", "0"},
	} {
		if os.Getenv(kv[0]) == "" {
			_ = os.Setenv(kv[0], kv[1])
		}
	}
}
