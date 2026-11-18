//go:build batontest

package main

import (
	"os"
	"testing"

	"baton/internal/desktop"
)

// In the test build, the Recycle Bin and the desktop app are faked before
// anything runs (go test -tags batontest). Unless the tester names a folder
// for the desktop app, there is none: not one that does not exist, which
// would still count as installed.
func TestTestBuildFakesOutsideWorld(t *testing.T) {
	for _, k := range []string{"BATON_RECYCLE", "BATON_DESKTOP_DATA", "BATON_DESKTOP_RUNNING"} {
		if os.Getenv(k) == "" {
			t.Errorf("%s is not set in the test build", k)
		}
	}
	t.Setenv("BATON_HOME", t.TempDir())
	t.Setenv("BATON_DESKTOP_DATA", "")
	fakeOutsideWorld()
	if d := desktop.DataDir(); d != "" {
		t.Errorf("the test build's own desktop app is %q, want none", d)
	}
}
