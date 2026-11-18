package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The test build fakes the Recycle Bin in baton's folder, or in the
// temporary folder without one, and says there is no Claude desktop app,
// unless the tester named other folders already.
func TestFakeOutsideWorld(t *testing.T) {
	home := t.TempDir()
	mine := filepath.Join(home, "my-desktop-data")
	t.Setenv("BATON_HOME", home)
	t.Setenv("BATON_RECYCLE", "")
	t.Setenv("BATON_DESKTOP_DATA", mine)
	t.Setenv("BATON_DESKTOP_RUNNING", "")
	fakeOutsideWorld()
	for k, want := range map[string]string{
		"BATON_RECYCLE":         filepath.Join(home, "test-recycle-bin"),
		"BATON_DESKTOP_DATA":    mine,
		"BATON_DESKTOP_RUNNING": "0",
	} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	t.Setenv("BATON_HOME", "")
	t.Setenv("BATON_RECYCLE", "")
	t.Setenv("BATON_DESKTOP_DATA", "")
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	fakeOutsideWorld()
	base := filepath.Join(os.TempDir(), "baton-test")
	for k, want := range map[string]string{
		"BATON_RECYCLE":         filepath.Join(base, "test-recycle-bin"),
		"BATON_DESKTOP_DATA":    "none",
		"BATON_DESKTOP_RUNNING": "1",
	} {
		if got := os.Getenv(k); got != want {
			t.Errorf("without BATON_HOME: %s = %q, want %q", k, got, want)
		}
	}
	// A folder that does not exist would still count as an installed app:
	// the window would offer its features, which then fail.
	t.Setenv("BATON_HOME", home)
	t.Setenv("BATON_DESKTOP_DATA", "")
	fakeOutsideWorld()
	if got := os.Getenv("BATON_DESKTOP_DATA"); got != "none" {
		t.Errorf("with BATON_HOME: BATON_DESKTOP_DATA = %q, want none", got)
	}
}
