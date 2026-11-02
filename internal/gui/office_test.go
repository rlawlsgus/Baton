package gui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/office"
	"baton/internal/run"
	"baton/internal/winproc"
)

func officeRow(v map[string]any) string {
	b, _ := json.Marshal(v)
	return string(b) + "\n"
}

func appendRows(t *testing.T, path string, rows ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(rows, "")); err != nil {
		t.Fatal(err)
	}
}

// The office: only with a token, only when turned on, and then a running
// baton session as a live view whose new transcript rows come to a held
// poll as events; the pop-out opens the office in a window of its own.
func TestOffice(t *testing.T) {
	r := newRig(t)
	t.Cleanup(r.srv.Close)
	if code, _ := r.do("GET", "/api/office", "", nil); code != http.StatusForbidden {
		t.Fatalf("office without a token: %d", code)
	}
	r.login()
	if code, _ := r.do("GET", "/api/office", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", code)
	}
	if code, body := r.do("GET", "/api/office", "", nil); code != http.StatusNotFound || !strings.Contains(body, `"code":"office_off"`) {
		t.Fatalf("office off: %d %s", code, body)
	}
	if code, body := r.do("POST", "/api/office/popout", "{}", nil); code != http.StatusNotFound || !strings.Contains(body, "office_off") {
		t.Fatalf("pop-out while off: %d %s", code, body)
	}
	if code, _ := r.do("POST", "/api/config", `{"key":"office","value":"true"}`, nil); code != 200 {
		t.Fatalf("turning the office on: %d", code)
	}
	_, body := r.do("GET", "/api/state", "", nil)
	var st stateView
	_ = json.Unmarshal([]byte(body), &st)
	if !st.Config.Office {
		t.Fatalf("setting not shown: %+v", st.Config)
	}
	for _, bad := range []string{"..%5Cx", "abc", "zzzzzzzz-zzzz", strings.Repeat("a", 65)} {
		if code, body := r.do("GET", "/api/office?session="+bad, "", nil); code != http.StatusBadRequest || !strings.Contains(body, "bad_session") {
			t.Fatalf("session %q: %d %s", bad, code, body)
		}
	}

	// A baton session on account "a", busy in its project.
	prof := filepath.Join(r.paths.ProfilesDir(), "a")
	_, _ = accounts.Update(r.paths.Accounts(), r.paths.AccountsLock(), func(reg *accounts.Registry) error {
		return reg.Add(accounts.Account{ID: "a", ProfileDir: prof, Enabled: true, LoggedIn: true})
	})
	proj := t.TempDir()
	const sid = "c1948d9b-2222-4333-8444-555555555555"
	tr := filepath.Join(prof, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	t0 := time.Now().Add(-10 * time.Second).UTC()
	ts := func(sec int) string {
		return t0.Add(time.Duration(sec) * time.Second).Format("2006-01-02T15:04:05.000Z")
	}
	n := 0
	uuid := func() string {
		n++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	}
	appendRows(t, tr,
		officeRow(map[string]any{"parentUuid": nil, "type": "user", "uuid": uuid(), "timestamp": ts(0), "cwd": proj, "entrypoint": "cli",
			"message": map[string]any{"role": "user", "content": "README 정리해 줘"}}),
		officeRow(map[string]any{"type": "api-request", "timestamp": ts(1)}),
		officeRow(map[string]any{"parentUuid": "x", "type": "assistant", "uuid": uuid(), "timestamp": ts(2), "cwd": proj,
			"message": map[string]any{"role": "assistant", "stop_reason": "tool_use", "content": []any{map[string]any{"type": "tool_use", "id": "toolu_1",
				"name": "Read", "input": map[string]any{"file_path": filepath.Join(proj, "README.md")}}}}}))
	pid := os.Getpid()
	_ = os.MkdirAll(filepath.Join(prof, "sessions"), 0o700)
	_ = os.WriteFile(filepath.Join(prof, "sessions", strconv.Itoa(pid)+".json"), []byte(`{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+sid+
		`","cwd":`+strconv.Quote(proj)+`,"status":"busy","entrypoint":"cli","name":"README 정리","startedAt":`+
		strconv.FormatInt(winproc.ProcessCreated(pid).UnixMilli(), 10)+`}`), 0o600)
	id := "20261030-101500-abc123"
	_ = os.MkdirAll(r.paths.Run(id), 0o700)
	_ = run.SaveJournal(r.paths.Run(id), run.Journal{RunID: id, SupervisorPID: pid, SupervisorCreated: winproc.ProcessCreated(pid),
		State: run.StateRunning, Account: "a", ChildPID: pid, SessionID: sid, TranscriptPath: tr, Cwd: proj, ConsoleWindow: 0x1234, Started: t0})

	code, body := r.do("GET", "/api/office?since=0&wait=0", "", nil)
	var resp office.Response
	if err := json.Unmarshal([]byte(body), &resp); code != 200 || err != nil {
		t.Fatalf("office: %d %s", code, body)
	}
	v := resp.View
	if v == nil || v.ID != sid || !resp.Reset || !v.Live || v.Source != "baton" || v.Account != "a" || v.AccountIndex != 0 ||
		v.RunID != id || v.RunState != "running" || !v.Window || v.Title != "README 정리" || v.Floor < 1 {
		t.Fatalf("view %+v", v)
	}
	if len(v.Actors) < 2 || v.Actors[0].ID != "main" || v.Actors[0].State != "working" || v.Actors[0].Room != "library" ||
		v.Actors[0].Detail != "README.md" || v.Actors[len(v.Actors)-1].Kind != "me" {
		t.Fatalf("actors %+v", v.Actors)
	}
	if len(resp.Events) == 0 || resp.Seq != resp.Events[len(resp.Events)-1].Seq || resp.Building.Total < 1 || !resp.Building.Floors[0].Live {
		t.Fatalf("events %+v building %+v", resp.Events, resp.Building)
	}
	// Paths stay private: the project folder, and file names only. (The body
	// is JSON, which doubles every backslash.)
	plain := strings.ReplaceAll(body, `\\`, `\`)
	if strings.Contains(plain, filepath.Join(proj, "README.md")) || strings.Contains(plain, tr) {
		t.Fatalf("a path leaked: %s", body)
	}

	// A held poll gets the rows written while it waits.
	type answer struct {
		code int
		body string
		took time.Duration
	}
	got := make(chan answer, 1)
	req, _ := http.NewRequest("GET", r.srv.Origin()+"/api/office?since="+strconv.FormatInt(resp.Seq, 10)+"&wait=1", nil)
	req.Header.Set(tokenHeader, r.token)
	req.Header.Set(pageHeader, r.page)
	req.Header.Set(seqHeader, "1000")
	go func() {
		start := time.Now()
		res, err := r.client.Do(req)
		if err != nil {
			got <- answer{code: -1, body: err.Error()}
			return
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		got <- answer{res.StatusCode, string(b), time.Since(start)}
	}()
	time.Sleep(400 * time.Millisecond)
	appendRows(t, tr, officeRow(map[string]any{"parentUuid": "x", "type": "user", "uuid": uuid(), "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "Exit code 1\nboom", "is_error": true}}}}))
	var a answer
	select {
	case a = <-got:
	case <-time.After(10 * time.Second):
		t.Fatal("the held poll never answered")
	}
	var next office.Response
	_ = json.Unmarshal([]byte(a.body), &next)
	if a.code != 200 || next.Reset || len(next.Events) == 0 || next.Events[0].Kind != "fail" || next.Events[0].Detail != "Exit code 1" || next.Events[0].Seq <= resp.Seq {
		t.Fatalf("held poll: %d %s", a.code, a.body)
	}
	if a.took < 300*time.Millisecond {
		t.Fatalf("answered before there was news (%v)", a.took)
	}

	// The pop-out: not available without a way to open a window; with one,
	// it opens a fresh one-time link to the office.
	if code, body := r.do("POST", "/api/office/popout", "{}", nil); code != http.StatusNotImplemented || !strings.Contains(body, "no_popout") {
		t.Fatalf("pop-out without OpenApp: %d %s", code, body)
	}
	var opened []string
	r.srv.opt.OpenApp = func(u string) error { opened = append(opened, u); return nil }
	if code, body := r.do("POST", "/api/office/popout", "{}", nil); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("pop-out: %d %s", code, body)
	}
	if len(opened) != 1 || !strings.HasPrefix(opened[0], r.srv.Origin()+"/?k=") || !strings.HasSuffix(opened[0], "#office") {
		t.Fatalf("opened %q", opened)
	}
	if code, token := r.exchange(strings.TrimSuffix(opened[0], "#office")); code != 200 || token == "" {
		t.Fatalf("the pop-out's code: %d", code)
	}

	// Closing the server ends a held poll at once.
	req2, _ := http.NewRequest("GET", r.srv.Origin()+"/api/office?since="+strconv.FormatInt(next.Seq, 10)+"&wait=1", nil)
	req2.Header = req.Header.Clone()
	req2.Header.Set(seqHeader, "1001")
	go func() {
		start := time.Now()
		res, err := r.client.Do(req2)
		if err != nil {
			got <- answer{code: -1, body: err.Error()}
			return
		}
		defer res.Body.Close()
		_, _ = io.ReadAll(res.Body)
		got <- answer{code: res.StatusCode, took: time.Since(start)}
	}()
	time.Sleep(300 * time.Millisecond)
	r.srv.Close()
	select {
	case a = <-got:
		if a.code != 200 || a.took > 3*time.Second {
			t.Fatalf("after Close: %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a held poll outlived the server")
	}
	if code, _ := r.do("GET", "/api/office", "", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("office after Close: %d", code)
	}
}
