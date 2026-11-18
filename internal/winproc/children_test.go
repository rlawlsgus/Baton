package winproc

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The processes this test starts are its children, with the command line
// they were started with; an ended one is not.
func TestChildrenAndCommandLine(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo baton-child-probe >nul & ping -n 60 127.0.0.1 >nul")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	kids := Children(os.Getpid())
	if !slices.ContainsFunc(kids, func(p Proc) bool { return p.PID == cmd.Process.Pid && strings.EqualFold(p.Exe, "cmd.exe") }) {
		t.Fatalf("children %v, no %d", kids, cmd.Process.Pid)
	}
	line, err := CommandLine(cmd.Process.Pid)
	if err != nil || !strings.Contains(line, "baton-child-probe") {
		t.Fatalf("command line %q, %v", line, err)
	}
	if self, err := CommandLine(os.Getpid()); err != nil || !strings.Contains(self, "-test.") {
		t.Fatalf("own command line %q, %v", self, err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	if slices.ContainsFunc(Children(os.Getpid()), func(p Proc) bool { return p.PID == cmd.Process.Pid }) {
		t.Fatal("an ended child is listed")
	}
	if _, err := CommandLine(0x7ffffff0); err == nil {
		t.Fatal("no such process, and no error")
	}
	if Children(0x7ffffff0) != nil {
		t.Fatal("children of no process")
	}
	if !IsConsoleHost("ConHost.exe") || IsConsoleHost("cmd.exe") {
		t.Fatal("console host")
	}
}
