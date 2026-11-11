package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doctor says whether Alt+V can paste pictures in a session: Claude Code
// reads the clipboard with Windows PowerShell, which it finds on PATH.
func TestDoctorChecksImagePaste(t *testing.T) {
	c := newCLI(t)
	out, _ := c.run([]string{"PATH=" + t.TempDir()}, "doctor")
	if !strings.Contains(out, "Windows PowerShell is not on PATH") {
		t.Fatalf("no PowerShell on PATH:\n%s", out)
	}
	ps := t.TempDir()
	if err := os.WriteFile(filepath.Join(ps, "powershell.exe"), []byte("MZ"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ = c.run([]string{"PATH=" + ps}, "doctor")
	if !strings.Contains(out, "image paste:  ok") {
		t.Fatalf("PowerShell on PATH:\n%s", out)
	}
}
