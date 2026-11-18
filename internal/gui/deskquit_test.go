package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"baton/internal/winproc"

	"golang.org/x/sys/windows"
)

// Switching while the desktop app runs, from the window: only when asked
// (quit), and only when nothing in the app is working; the answer lists
// what is. baton then quits the app, switches, and opens it again (opening
// does nothing in a test). The app here is the stand-in file
// BATON_DESKTOP_RUNNING names, which quitting deletes.
func TestDesktopLoginsQuitFirst(t *testing.T) {
	r, app := newLoginsRig(t)
	r.want("/api/config", `{"key":"desktop_logins","value":"true","acknowledge":true}`, http.StatusOK, "")
	r.want("/api/desktop/logins/add", `{"open":false}`, http.StatusOK, "")
	app.signIn(dlB, "B")
	r.want("/api/desktop/logins/use", `{"account":"`+dlA+`"}`, http.StatusOK, "") // on A, A and B kept

	running := filepath.Join(t.TempDir(), "app-running")
	if err := os.WriteFile(running, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "file:"+running)
	if st := r.loginsState(); !st.DesktopLogins.Running {
		t.Fatalf("not running: %+v", st)
	}
	r.want("/api/desktop/logins/use", `{"account":"`+dlB+`"}`, http.StatusConflict, "dl_app_running")

	// Claude works in a session of the app's (this test process stands in
	// for its Claude Code): nothing is quit, and the answer says where.
	entry := registerBusy(t, "fix the login bug")
	resp := r.want("/api/desktop/logins/use", `{"account":"`+dlB+`","quit":true}`, http.StatusConflict, "dl_app_working")
	var refused struct {
		Working []struct {
			Title string `json:"title"`
			What  string `json:"what"`
		} `json:"working"`
		Quit bool `json:"quit"`
	}
	if json.Unmarshal([]byte(resp), &refused) != nil || len(refused.Working) != 1 || refused.Working[0].Title != "fix the login bug" ||
		refused.Working[0].What != "turn" || refused.Quit {
		t.Fatalf("working: %s", resp)
	}
	if _, err := os.Stat(running); err != nil || !strings.Contains(app.read("config.json"), `"sealed2-A"`) {
		t.Fatalf("quit or switched while working: %v", err)
	}
	r.want("/api/desktop/logins/add", `{"open":false,"quit":true}`, http.StatusConflict, "dl_app_working")

	// Done working: quit, switched, opened again whatever open said.
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	resp = r.want("/api/desktop/logins/use", `{"account":"`+dlB+`","open":false,"quit":true}`, http.StatusOK, "")
	if !strings.Contains(resp, `"quit":true`) || !strings.Contains(resp, `"opened":true`) || !strings.Contains(resp, `"to":"`+dlB+`"`) {
		t.Fatalf("use: %s", resp)
	}
	if _, err := os.Stat(running); !os.IsNotExist(err) {
		t.Fatalf("the app was not quit: %v", err)
	}
	if !strings.Contains(app.read("config.json"), `"sealed2-B"`) || app.read(filepath.Join("Network", "Cookies")) != "cookies-B" {
		t.Fatal("not on B")
	}

	// Not running: quit asks nothing more than a switch does.
	resp = r.want("/api/desktop/logins/use", `{"account":"`+dlA+`","quit":true}`, http.StatusOK, "")
	if strings.Contains(resp, `"quit"`) || strings.Contains(resp, `"opened"`) {
		t.Fatalf("use with the app quit: %s", resp)
	}
}

// registerBusy registers this test process in the user's Claude Code
// folder as a session of the desktop app's where Claude works; it returns
// the entry's file.
func registerBusy(t *testing.T, name string) string {
	t.Helper()
	pid := os.Getpid()
	born := winproc.ProcessCreated(pid)
	ft := windows.NsecToFiletime(born.UnixNano())
	body := fmt.Sprintf(`{"pid":%d,"sessionId":"s1","cwd":"C:\\proj","startedAt":%d,"procStart":"%d","kind":"interactive","entrypoint":"claude-desktop","name":%q,"status":"busy"}`,
		pid, born.UnixMilli(), int64(ft.HighDateTime)<<32|int64(ft.LowDateTime), name)
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
