package run

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/winproc"
)

const delID = "11111111-2222-4333-8444-555555555555"

// writeMark puts a marker as another delete would have left it.
func writeMark(t *testing.T, root string, m deletingMark) string {
	t.Helper()
	p := deletingPath(root, delID)
	data, _ := json.Marshal(m)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// One delete of a conversation at a time; the marker goes with release, a
// second release is harmless, and ids are not case-sensitive.
func TestMarkDeletingExcludesAndReleases(t *testing.T) {
	root := t.TempDir()
	if Deleting(root, delID) {
		t.Fatal("nothing marked yet")
	}
	release, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatal(err)
	}
	if !Deleting(root, delID) || !Deleting(root, strings.ToUpper(delID)) {
		t.Fatal("the marker does not hold")
	}
	_, err = MarkDeleting(root, strings.ToUpper(delID))
	var de *DeletingError
	if !errors.Is(err, ErrDeleting) || !errors.As(err, &de) || de.PID != os.Getpid() || time.Since(de.Since) > time.Minute {
		t.Fatalf("second delete: %v", err)
	}
	release()
	release()
	if Deleting(root, delID) {
		t.Fatal("still held after release")
	}
	if _, err := os.Stat(filepath.Join(root, "deleting", delID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker left: %v", err)
	}
	again, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again()

	for _, bad := range []string{"", "../x", `..\` + delID, delID + `\x`} {
		if _, err := MarkDeleting(root, bad); err == nil || errors.Is(err, ErrDeleting) {
			t.Errorf("%q: %v", bad, err)
		}
		if Deleting(root, bad) {
			t.Errorf("%q counts as being deleted", bad)
		}
	}
}

// A marker whose process is gone (or is another process under the same
// pid), or that is older than two minutes, holds nothing: a later delete
// takes it over. So does one from ahead of the clock. One still being
// written holds while it is new.
func TestMarkDeletingTakesOverStaleMarkers(t *testing.T) {
	pid := os.Getpid()
	created := winproc.ProcessCreated(pid)
	if created.IsZero() {
		t.Fatal("no creation time for this process")
	}
	now := time.Now()
	for _, c := range []struct {
		name string
		m    deletingMark
	}{
		{"no such process", deletingMark{PID: 0x7ffffffc, Created: created, At: now}},
		{"pid used again", deletingMark{PID: pid, Created: created.Add(-time.Hour), At: now}},
		{"older than two minutes", deletingMark{PID: pid, Created: created, At: now.Add(-3 * time.Minute)}},
		{"clock set back", deletingMark{PID: pid, Created: created, At: now.Add(time.Hour)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeMark(t, root, c.m)
			if Deleting(root, delID) {
				t.Fatal("a stale marker holds")
			}
			release, err := MarkDeleting(root, delID)
			if err != nil {
				t.Fatalf("not taken over: %v", err)
			}
			if !Deleting(root, delID) {
				t.Fatal("the new marker does not hold")
			}
			release()
		})
	}

	// Being written: empty for a moment.
	root := t.TempDir()
	p := deletingPath(root, delID)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !Deleting(root, delID) {
		t.Fatal("a marker being written does not hold")
	}
	if _, err := MarkDeleting(root, delID); !errors.Is(err, ErrDeleting) {
		t.Fatalf("over a marker being written: %v", err)
	}
	// ...and never finished.
	old := now.Add(-3 * time.Minute)
	_ = os.Chtimes(p, old, old)
	release, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatalf("an unfinished old marker: %v", err)
	}
	release()
}

// A delete that hung past two minutes lost its marker to a later one: its
// release leaves the later one's marker alone.
func TestReleaseKeepsALaterDeletesMarker(t *testing.T) {
	root := t.TempDir()
	first, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatal(err)
	}
	p := deletingPath(root, delID)
	var m deletingMark
	data, _ := os.ReadFile(p)
	if err := json.Unmarshal(data, &m); err != nil || m.PID != os.Getpid() {
		t.Fatalf("marker %s: %v", data, err)
	}
	m.At = m.At.Add(-3 * time.Minute)
	writeMark(t, root, m)
	second, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatalf("taking over a hung delete's marker: %v", err)
	}
	first()
	if !Deleting(root, delID) {
		t.Fatal("the hung delete's release removed the later marker")
	}
	second()
	if Deleting(root, delID) {
		t.Fatal("the later release did not remove its marker")
	}
}

// Another process deleting the conversation holds it while it runs; once
// it is gone without releasing (killed), the next delete takes over.
func TestMarkDeletingHeldByAnotherProcess(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHoldMarkerHelper$")
	cmd.Env = append(os.Environ(), "BATON_TEST_HOLD_MARKER="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "held" {
		t.Fatalf("helper: %q", line)
	}
	if !Deleting(root, delID) {
		t.Fatal("another live process's marker does not hold")
	}
	_, err = MarkDeleting(root, delID)
	var de *DeletingError
	if !errors.As(err, &de) || de.PID != cmd.Process.Pid {
		t.Fatalf("while another process deletes it: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	if Deleting(root, delID) {
		t.Fatal("a killed process's marker still holds")
	}
	release, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatalf("after the other process died: %v", err)
	}
	release()
}

// TestHoldMarkerHelper is the other process of
// TestMarkDeletingHeldByAnotherProcess: it takes the marker and keeps it
// until it is killed.
func TestHoldMarkerHelper(t *testing.T) {
	root := os.Getenv("BATON_TEST_HOLD_MARKER")
	if root == "" {
		t.Skip("run by TestMarkDeletingHeldByAnotherProcess")
	}
	if _, err := MarkDeleting(root, delID); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("held")
	_, _ = io.ReadAll(os.Stdin)
	os.Exit(0)
}

// AnyDeleting sees a live delete of any conversation, and not the note a
// delete that stopped half way leaves; the note comes and goes on its own.
func TestAnyDeletingAndLeft(t *testing.T) {
	root := t.TempDir()
	if AnyDeleting(root) || Left(root, delID) {
		t.Fatal("nothing yet")
	}
	if err := NoteLeft(root, strings.ToUpper(delID)); err != nil {
		t.Fatal(err)
	}
	if !Left(root, delID) || AnyDeleting(root) {
		t.Fatalf("left %v, deleting %v", Left(root, delID), AnyDeleting(root))
	}
	release, err := MarkDeleting(root, delID)
	if err != nil {
		t.Fatal(err)
	}
	if !AnyDeleting(root) {
		t.Fatal("a delete runs")
	}
	release()
	if AnyDeleting(root) || !Left(root, delID) {
		t.Fatal("after the delete")
	}
	ClearLeft(root, delID)
	if Left(root, delID) {
		t.Fatal("cleared")
	}
	if NoteLeft(root, "nope") == nil || Left(root, "nope") {
		t.Fatal("not a session id")
	}
}
