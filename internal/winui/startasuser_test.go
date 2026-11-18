package winui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A program started as the user would start it gets the user's own
// environment, not this process's: a variable only this process has is
// not passed on. The program here is cmd.exe writing out its environment
// to a file in the test's folder, then ending.
func TestStartAsUserLeavesThisEnvironmentBehind(t *testing.T) {
	t.Setenv("BATON_START_AS_USER_PROBE", "only-here")
	out := filepath.Join(t.TempDir(), "env.txt")
	if strings.ContainsAny(out, " &^") {
		t.Skip("the test folder's name needs quoting for cmd")
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if err := StartAsUser(cmdExe, "/d", "/c", "set", ">", out); err != nil {
		t.Fatal(err)
	}
	// Read until it is written whole: windir comes last, and it reads the
	// same twice.
	var env, last string
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(150 * time.Millisecond) {
		b, err := os.ReadFile(out)
		if err == nil && strings.Contains(strings.ToUpper(string(b)), "\nWINDIR=") && string(b) == last {
			env = last
			break
		}
		last = string(b)
	}
	if env == "" {
		t.Fatal("cmd.exe wrote no environment")
	}
	up := strings.ToUpper(env)
	if strings.Contains(up, "BATON_START_AS_USER_PROBE") {
		t.Fatal("this process's own variable was passed on")
	}
	if !strings.Contains(up, "USERPROFILE=") || !strings.Contains(up, "LOCALAPPDATA=") {
		t.Fatalf("the user's own variables are missing:\n%s", env)
	}
}
