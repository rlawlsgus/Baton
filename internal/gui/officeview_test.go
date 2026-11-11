package gui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/office"
)

// The office keeps its side columns per kind of window and what the user
// hid in config.json; the layout comes back in /api/state, the hidden flags
// in the next office poll.
func TestOfficeLayoutAndHide(t *testing.T) {
	r := newRig(t)
	t.Cleanup(r.srv.Close)
	r.login()
	for p, b := range map[string]string{"/api/office/layout": `{"window":"tab","left":250}`,
		"/api/office/hide": `{"session":"11111111-2222-4333-8444-555555555555","hidden":true}`} {
		if code, body := r.do("POST", p, b, nil); code != http.StatusNotFound || !strings.Contains(body, "office_off") {
			t.Fatalf("%s while off: %d %s", p, code, body)
		}
	}
	if code, _ := r.do("POST", "/api/config", `{"key":"office","value":"true"}`, nil); code != 200 {
		t.Fatalf("turning the office on: %d", code)
	}

	// The layout, per window.
	for _, c := range []struct {
		body string
		code int
	}{
		{`{"window":"tab","left":250,"right":280,"right_folded":true,"last":"l"}`, 200},
		{`{"window":"popout","left":99999,"last":"x"}`, 200},
		{`{"window":"side","left":200}`, http.StatusBadRequest},
		{`{"left":200}`, http.StatusBadRequest},
	} {
		if code, body := r.do("POST", "/api/office/layout", c.body, nil); code != c.code {
			t.Fatalf("layout %s: %d %s", c.body, code, body)
		}
	}
	_, body := r.do("GET", "/api/state", "", nil)
	var st stateView
	_ = json.Unmarshal([]byte(body), &st)
	if l := st.Config.OfficeLayout; l.Tab != (config.OfficePanes{Left: 250, Right: 280, RightFolded: true, Last: "l"}) || l.Popout != (config.OfficePanes{Left: config.OfficeWidthMax}) {
		t.Fatalf("layout in state: %+v", l)
	}
	if !strings.Contains(body, `"office_layout":{"tab":{"left":250,"right":280,"right_folded":true,"last":"l"},"popout":{"left":1200}}`) {
		t.Fatalf("state json: %s", body)
	}

	// A conversation in the user's own setup, for the building.
	home := os.Getenv("CLAUDE_CONFIG_DIR")
	proj := filepath.Join(r.paths.Root, "Proj")
	const sid = "c1948d9b-2222-4333-8444-555555555555"
	tr := filepath.Join(home, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(tr), 0o700)
	ts := time.Now().Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")
	appendRows(t, tr,
		officeRow(map[string]any{"parentUuid": nil, "type": "user", "uuid": "00000000-0000-4000-8000-000000000001", "timestamp": ts, "cwd": proj,
			"message": map[string]any{"role": "user", "content": "hello"}}),
		officeRow(map[string]any{"parentUuid": "x", "type": "assistant", "uuid": "00000000-0000-4000-8000-000000000002", "timestamp": ts, "cwd": proj,
			"message": map[string]any{"role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": "hi"}}}}))
	poll := func(want func(office.Response) bool) office.Response {
		t.Helper()
		var resp office.Response
		for i := 0; i < 60; i++ {
			code, body := r.do("GET", "/api/office?since=0&wait=0", "", nil)
			resp = office.Response{} // flags left out when false must not stay from the last answer
			if err := json.Unmarshal([]byte(body), &resp); code != 200 || err != nil {
				t.Fatalf("office: %d %s", code, body)
			}
			if want(resp) {
				return resp
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("office never got there: view %+v building %+v", resp.View, resp.Building)
		return resp
	}
	resp := poll(func(x office.Response) bool { return x.View != nil && x.View.ID == sid })
	if f := resp.Building.Floors[0]; f.Hidden || f.Count != 1 || f.Shown != 1 || f.Sessions[0].Hidden {
		t.Fatalf("before hiding: %+v", f)
	}

	// Hiding: bad requests change nothing.
	for _, bad := range []string{`{"session":"..\\x","hidden":true}`, `{"hidden":true}`, `{"all":true,"hidden":true}`,
		`{"changes":[{"session":"` + sid + `","floor":"c:\\p","hidden":true}]}`, `{"floor":"` + strings.Repeat("x", 1100) + `","hidden":true}`} {
		if code, body := r.do("POST", "/api/office/hide", bad, nil); code != http.StatusBadRequest {
			t.Fatalf("hide %s: %d %s", bad, code, body)
		}
	}
	if cfg, _ := config.Load(r.paths.Config()); cfg.OfficeView.HiddenSessions != nil || cfg.OfficeView.HiddenFloors != nil {
		t.Fatalf("a bad request was written: %+v", cfg.OfficeView)
	}
	if code, body := r.do("POST", "/api/office/hide", `{"session":"`+sid+`","hidden":true}`, nil); code != 200 {
		t.Fatalf("hide session: %d %s", code, body)
	}
	resp = poll(func(x office.Response) bool {
		return x.View == nil && len(x.Building.Floors) == 1 && x.Building.Floors[0].Sessions[0].Hidden
	})
	if f := resp.Building.Floors[0]; f.Hidden || f.Shown != 0 || f.Count != 1 {
		t.Fatalf("session hidden: %+v", f)
	}
	// Asked for by id, it is still shown.
	if code, body := r.do("GET", "/api/office?session="+sid+"&since=0&wait=0", "", nil); code != 200 || !strings.Contains(body, `"id":"`+sid+`"`) {
		t.Fatalf("pinned hidden session: %d", code)
	}

	// The floor too (by its folder, in any case), and the session shown in
	// the same request; then everything again.
	if code, body := r.do("POST", "/api/office/hide", `{"floor":`+jsonString(strings.ToUpper(proj))+`,"hidden":true,"changes":[{"session":"`+sid+`","hidden":false}]}`, nil); code != 200 {
		t.Fatalf("hide floor: %d %s", code, body)
	}
	cfg, _ := config.Load(r.paths.Config())
	if v := cfg.OfficeView; v == nil || len(v.HiddenSessions) != 0 || len(v.HiddenFloors) != 1 || v.HiddenFloors[0] != office.ProjectKey(proj) || v.Tab.Left != 250 {
		t.Fatalf("config after hiding the floor: %+v", cfg.OfficeView)
	}
	resp = poll(func(x office.Response) bool { return len(x.Building.Floors) == 1 && x.Building.Floors[0].Hidden })
	if f := resp.Building.Floors[0]; resp.View != nil || f.Sessions[0].Hidden || f.Shown != 1 {
		t.Fatalf("floor hidden: view %+v floor %+v", resp.View, f)
	}
	if code, body := r.do("POST", "/api/office/hide", `{"all":true,"hidden":false}`, nil); code != 200 {
		t.Fatalf("show everything: %d %s", code, body)
	}
	poll(func(x office.Response) bool { return x.View != nil && x.View.ID == sid && !x.Building.Floors[0].Hidden })
	cfg, _ = config.Load(r.paths.Config())
	if v := cfg.OfficeView; v == nil || v.HiddenFloors != nil || v.HiddenSessions != nil || v.Popout.Left != config.OfficeWidthMax {
		t.Fatalf("config after showing everything: %+v", cfg.OfficeView)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
