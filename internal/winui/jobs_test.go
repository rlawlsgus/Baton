package winui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func cmdExe() string { return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe") }

// StartDetached must really start the program, with this environment (the
// relaunched window and the uninstall helper need LOCALAPPDATA).
func TestStartDetachedStartsAChild(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	t.Setenv("BATON_TEST_MARK", "detached-ok")
	if err := StartDetached(cmdExe(), "/d", "/c", "echo %BATON_TEST_MARK%>"+out); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(out); err == nil && strings.Contains(string(b), "detached-ok") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the detached child did not run with the environment")
}

func TestStartDetachedReportsAMissingProgram(t *testing.T) {
	err := StartDetached(filepath.Join(t.TempDir(), "missing.exe"))
	if err == nil || strings.Contains(err.Error(), "already started") || strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("err = %v", err)
	}
}

// The fallback start builds a new command: an exec.Cmd cannot be started twice.
func TestStartWithFallbackBuildsAFreshCommand(t *testing.T) {
	calls := 0
	_, err := startWithFallback(func() *exec.Cmd {
		calls++
		return exec.Command(filepath.Join(t.TempDir(), "missing.exe"))
	}, windows.CREATE_NO_WINDOW)
	// Inside a job that refuses breakaway (as under some launchers, and
	// this test runner), the first attempt is denied and a second one runs.
	if err == nil || strings.Contains(err.Error(), "already started") || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v (%d attempts)", err, calls)
	}
	if calls > 2 {
		t.Fatalf("%d attempts", calls)
	}
}

func TestDeleteAfterExit(t *testing.T) {
	f := filepath.Join(t.TempDir(), "helper copy.exe")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAfterExit(f); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the file was not deleted")
}

// CloseAll ends a running command; Run must not close the job handle again.
func TestJobsCloseAllEndsRunningCommands(t *testing.T) {
	j := &Jobs{}
	done := make(chan int, 1)
	go func() {
		_, code, _ := j.Run(cmdExe(), []string{"/d", "/c", "ping -n 30 127.0.0.1 >nul"}, os.Environ(), time.Minute)
		done <- code
	}()
	time.Sleep(1500 * time.Millisecond)
	j.CloseAll()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the command outlived CloseAll")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.open) != 0 {
		t.Fatalf("jobs left open: %d", len(j.open))
	}
}
