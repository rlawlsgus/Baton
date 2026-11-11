package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/winproc"
)

// Every folder in these tests is under t.TempDir(): the desktop app's data
// folder, the user's own Claude Code folder and baton's. The write guard
// would panic on anything else.

const (
	acctA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	orgA  = "a0000000-0000-4000-8000-00000000000a"
	acctB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	orgB  = "b0000000-0000-4000-8000-00000000000b"
	acctX = "cccccccc-cccc-4ccc-8ccc-cccccccccccc" // the desktop app's own account, none of baton's
	orgX  = "c0000000-0000-4000-8000-00000000000c"
	sid   = "5e550000-0000-4000-8000-000000000001"
	sid2  = "5e550000-0000-4000-8000-000000000002"
)

var (
	keyA = acctA + "/" + orgA
	keyB = acctB + "/" + orgB
	keyX = acctX + "/" + orgX
)

type rig struct {
	t       *testing.T
	root    string
	data    string // the desktop app's data folder
	home    string // the user's own Claude Code folder (~/.claude)
	prof    string // the profile of baton's account a
	proj    string // the project folder
	running bool
	d       *Desk
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{t: t, root: root, data: filepath.Join(root, "Claude-Data", "claude_test"),
		home: filepath.Join(root, "user", ".claude"), prof: filepath.Join(root, "baton", "profiles", "a"),
		proj: filepath.Join(root, "Proj")}
	for _, d := range []string{filepath.Join(r.data, sessionsDir, acctA, orgA), r.home, r.prof, r.proj} {
		must(t, os.MkdirAll(d, 0o700))
	}
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctA, "other": "never read"})
	accts := []accounts.Account{
		{ID: "a", ProfileDir: r.prof, Priority: 30, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "a@example.com",
			AccountUUID: acctA, OrgID: orgA, OrgName: "A's org"},
		{ID: "b", ProfileDir: filepath.Join(root, "baton", "profiles", "b"), Priority: 20, Enabled: true, AuthMethod: "none",
			Email: "", AccountUUID: acctB, OrgID: orgB}, // signed out: still the same Claude account
		{ID: "a2", ProfileDir: filepath.Join(root, "baton", "profiles", "a2"), Priority: 15, LoggedIn: true, AuthMethod: "claude.ai",
			Email: "a@example.com", AccountUUID: acctA, OrgID: orgA}, // the same account as a
		{ID: "key", ProfileDir: filepath.Join(root, "baton", "profiles", "key"), Priority: 10, LoggedIn: true, AuthMethod: "api_key"},
		{ID: "cloud", ProfileDir: filepath.Join(root, "baton", "profiles", "cloud"), Priority: 5, LoggedIn: true, AuthMethod: "bedrock"},
		{ID: "old", ProfileDir: filepath.Join(root, "baton", "profiles", "old"), Priority: 1, LoggedIn: true, AuthMethod: "claude.ai",
			Email: "old@example.com"}, // checked before baton read account ids
	}
	r.d = &Desk{IndexPath: filepath.Join(root, "baton", "desktop.json"), LockPath: filepath.Join(root, "baton", "desktop.lock"),
		UserHome: r.home, RunsDir: filepath.Join(root, "baton", "runs"), Accounts: accts, Running: func() bool { return r.running },
		Now: func() time.Time { return time.UnixMilli(1_800_000_000_000) }}
	r.d.SetData(r.data)
	return r
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) writeJSON(path string, v any) {
	r.t.Helper()
	b, _ := json.Marshal(v)
	must(r.t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(r.t, os.WriteFile(path, b, 0o600))
}

// conversation writes conversation id in profile dir, two turns, and returns its file.
func (r *rig) conversation(dir, id string) string {
	r.t.Helper()
	p := filepath.Join(dir, "projects", claude.ProjectSlug(r.proj), id+".jsonl")
	must(r.t, os.MkdirAll(filepath.Dir(p), 0o700))
	cwd := strconv.Quote(r.proj)
	rows := `{"type":"permission-mode","permissionMode":"default","sessionId":"` + id + `"}` + "\n" +
		`{"type":"user","cwd":` + cwd + `,"timestamp":"2026-11-01T10:00:00.000Z","message":{"role":"user","content":"fix the <login> bug"}}` + "\n" +
		`{"type":"assistant","cwd":` + cwd + `,"timestamp":"2026-11-01T10:05:00.000Z","message":{"role":"assistant","content":"done"}}` + "\n"
	must(r.t, os.WriteFile(p, []byte(rows), 0o600))
	return p
}

func (r *rig) placeDir(key string) string {
	acct, org, _ := strings.Cut(key, "/")
	return filepath.Join(r.data, sessionsDir, acct, org)
}

func (r *rig) record(key, id string) map[string]any {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.placeDir(key), id+".json"))
	if err != nil {
		return nil
	}
	m := map[string]any{}
	must(r.t, json.Unmarshal(b, &m))
	return m
}

func (r *rig) index() Index {
	r.t.Helper()
	ix, err := LoadIndex(r.d.IndexPath)
	must(r.t, err)
	return ix
}

func (r *rig) view(id, file string) View {
	r.t.Helper()
	v, err := r.d.Places(Conversation{ID: id, Transcript: file})
	must(r.t, err)
	return v
}

func rowOf(v View, key string) Row {
	for _, x := range v.Rows {
		if x.Key == key {
			return x
		}
	}
	return Row{}
}

func (r *rig) apply(id, file string, keys ...string) Result {
	r.t.Helper()
	res, err := r.d.Apply(Conversation{ID: id, Transcript: file}, keys)
	must(r.t, err)
	return res
}

// live registers a Claude Code of the desktop app in the user's own folder:
// this test's own process stands in for it.
func (r *rig) live(sessionID, host string) string {
	r.t.Helper()
	created := winproc.ProcessCreated(os.Getpid())
	p := filepath.Join(r.home, "sessions", strconv.Itoa(os.Getpid())+".json")
	r.writeJSON(p, map[string]any{"pid": os.Getpid(), "sessionId": sessionID, "cwd": r.proj, "kind": "interactive",
		"status": "idle", "entrypoint": "claude-desktop", "startedAt": created.UnixMilli() + 500, "hostSessionId": host})
	return p
}

// The places: one per Claude account (several of baton's accounts on one
// are one row), the desktop app's own account, and accounts that cannot be
// chosen, with why.
func TestPlaces(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	v := r.view(sid, file)
	if v.Title != "fix the <login> bug" || v.Split || v.Problem != "" || v.AppRunning {
		t.Fatalf("view: %+v", v)
	}
	a := rowOf(v, keyA)
	if !slices.Equal(a.Accounts, []string{"a", "a2"}) || !a.Active || !a.Used || a.Checked || a.Disabled || a.Email != "a@example.com" {
		t.Fatalf("row a: %+v", a)
	}
	if b := rowOf(v, keyB); !slices.Equal(b.Accounts, []string{"b"}) || b.Active || b.Used || b.Disabled {
		t.Fatalf("row b (signed out, never used in the app): %+v", b)
	}
	why := map[string]Row{}
	for _, x := range v.Rows {
		if x.Key == "" {
			why[x.Accounts[0]] = x
		}
	}
	if why["key"].Why != "api_key" || why["cloud"].Why != "cloud" || why["cloud"].Provider != "bedrock" || why["old"].Why != "unknown" ||
		!why["key"].Disabled || len(v.Rows) != 5 {
		t.Fatalf("rows that cannot be chosen: %+v", v.Rows)
	}

	// The desktop app signed in to an account none of baton's is: offered,
	// named by the email the user's own Claude Code has for it.
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctX})
	must(t, os.MkdirAll(r.placeDir(keyX), 0o700))
	r.writeJSON(filepath.Join(filepath.Dir(r.home), ".claude.json"), map[string]any{"oauthAccount": map[string]any{
		"accountUuid": acctX, "organizationUuid": orgX, "emailAddress": "desk@example.com"}})
	v = r.view(sid, file)
	if x := rowOf(v, keyX); !x.AppOnly || !x.Active || !x.Used || x.Email != "desk@example.com" || rowOf(v, keyA).Active {
		t.Fatalf("the app's own account: %+v", v.Rows)
	}
	// Its organization is not plain (two lists, nothing names one): not offered.
	must(t, os.Remove(filepath.Join(filepath.Dir(r.home), ".claude.json")))
	must(t, os.MkdirAll(filepath.Join(r.data, sessionsDir, acctX, orgA), 0o700))
	if v = r.view(sid, file); rowOf(v, keyX).Key != "" || len(v.Rows) != 5 {
		t.Fatalf("an account with two organizations was offered: %+v", v.Rows)
	}
}

// A shown session gets a record shaped exactly like the desktop app's own
// import record, written through a temporary name the app never promotes,
// and its conversation file a second name in the user's own folder.
func TestApplyWritesRecordAndLink(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	res := r.apply(sid, file, keyA, keyB)
	if !slices.Equal(res.Added, []string{keyA, keyB}) || len(res.Removed) != 0 || !res.ActiveAdded || !res.OtherAdded || res.AppRunning {
		t.Fatalf("result: %+v", res)
	}
	want := `{"sessionId":"local_` + sid + `","cliSessionId":"` + sid + `","cwd":` + strconv.Quote(r.proj) + `,"originCwd":` + strconv.Quote(r.proj) +
		`,"createdAt":1793527200000,"lastActivityAt":1793527500000,"indexedAt":1800000000000,"isArchived":false,` +
		`"title":"fix the <login> bug","titleSource":"auto","sessionPermissionUpdates":[],"alwaysAllowedReasons":[],` +
		`"adoptedFromOtherSurface":true,"fromCliTranscript":true}`
	for _, k := range []string{keyA, keyB} {
		got, err := os.ReadFile(filepath.Join(r.placeDir(k), "local_"+sid+".json"))
		must(t, err)
		if string(got) != want {
			t.Fatalf("record in %s:\n got %s\nwant %s", k, got, want)
		}
		left, _ := filepath.Glob(filepath.Join(r.placeDir(k), "*.tmp"))
		if len(left) != 0 {
			t.Fatalf("temporary files left: %v", left)
		}
	}
	second := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	if !sameFile(second, file) {
		t.Fatal("the conversation has no second name in the user's own folder")
	}
	// A turn on either side is in both names.
	f, err := os.OpenFile(second, os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	_, _ = f.WriteString(`{"type":"user","message":{"role":"user","content":"from the desktop app"}}` + "\n")
	f.Close()
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "from the desktop app") {
		t.Fatal("a turn through the second name is not in baton's file")
	}
	v := r.view(sid, file)
	if !rowOf(v, keyA).Checked || !rowOf(v, keyB).Checked {
		t.Fatalf("not shown as ticked: %+v", v.Rows)
	}
	b := r.index().Badges()[sid]
	if !slices.Equal(b.Places, []string{"a, a2", "b"}) || b.Split {
		t.Fatalf("badge: %+v", b)
	}
	// The listings show it once, under its account.
	list := claude.ListAllTranscripts([]string{r.prof, r.home}, 10)
	if len(list) != 1 || list[0].ConfigDir != r.prof {
		t.Fatalf("listed twice or under the wrong folder: %+v", list)
	}
	if got := claude.FindTranscript([]string{r.prof, r.home}, sid); got != file {
		t.Fatalf("FindTranscript = %s, want the profile's name", got)
	}

	// Unticked everywhere: baton's records go, then the second name.
	res = r.apply(sid, file)
	if !slices.Equal(res.Removed, []string{keyA, keyB}) || res.ActiveRemoved { // the app is closed: gone at once
		t.Fatalf("result: %+v", res)
	}
	if r.record(keyA, "local_"+sid) != nil || r.record(keyB, "local_"+sid) != nil || isFile(second) || !isFile(file) {
		t.Fatal("hiding left a record or the second name, or removed baton's file")
	}
	if len(r.index().Threads) != 0 {
		t.Fatalf("index keeps a thread with nothing left: %+v", r.index().Threads)
	}
}

// The temporary name never matches local_*.json.tmp, which the desktop
// app turns into a record when it finds one left over.
func TestTemporaryName(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		f, err := os.CreateTemp(dir, ".baton-*.tmp")
		must(t, err)
		name := filepath.Base(f.Name())
		f.Close()
		if ok, _ := filepath.Match("local_*.json.tmp", name); ok || strings.HasSuffix(name, ".json.tmp") || strings.HasSuffix(name, ".json") {
			t.Fatalf("temporary name %s looks like a record", name)
		}
	}
	p := filepath.Join(dir, "local_x.json")
	must(t, writeRecord(p, []byte("{}"), false))
	if err := writeRecord(p, []byte(`{"a":1}`), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("a new record replaced a file: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{}" {
		t.Fatalf("file changed: %s", b)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".baton-*")); len(left) != 5 {
		t.Fatalf("temporary file left after a refused write: %v", left)
	}
}

// A record the desktop app made itself is never changed, archived or
// removed, and the row for it is ticked and locked (an archived one: locked).
func TestDesktopRecordNeverTouched(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	own := filepath.Join(r.placeDir(keyA), "local_d0000000-0000-4000-8000-0000000000d0.json")
	content := `{"sessionId":"local_d0000000-0000-4000-8000-0000000000d0","cliSessionId":"` + sid + `","cwd":"x","title":"mine"}`
	must(t, os.WriteFile(own, []byte(content), 0o600))
	// Another of the app's records already named after the conversation id.
	named := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	must(t, os.MkdirAll(filepath.Dir(named), 0o700))
	must(t, os.WriteFile(named, []byte(`{"sessionId":"local_`+sid+`","cliSessionId":"`+sid2+`","cwd":"x"}`), 0o600))
	v := r.view(sid, file)
	if a := rowOf(v, keyA); !a.Checked || !a.Locked || a.Why != "own" {
		t.Fatalf("own record row: %+v", a)
	}
	if b := rowOf(v, keyB); b.Checked || b.Locked {
		t.Fatalf("a record of another conversation counted: %+v", b)
	}
	res := r.apply(sid, file, keyB) // a stays as it is either way
	if !slices.Equal(res.Added, []string{keyB}) || len(res.Removed) != 0 {
		t.Fatalf("result: %+v", res)
	}
	if b, _ := os.ReadFile(own); string(b) != content {
		t.Fatalf("the app's own record changed: %s", b)
	}
	recs := listRecords(r.placeDir(keyB))
	if len(recs) != 2 {
		t.Fatalf("baton's record did not take another name: %v", recs)
	}
	r.apply(sid, file)
	if b, _ := os.ReadFile(own); string(b) != content {
		t.Fatalf("the app's own record changed: %s", b)
	}
	if b, _ := os.ReadFile(named); !strings.Contains(string(b), sid2) || len(listRecords(r.placeDir(keyB))) != 1 {
		t.Fatal("unticking touched a record of the app's")
	}
	must(t, os.WriteFile(own, []byte(strings.Replace(content, `"title"`, `"isArchived":true,"title"`, 1)), 0o600))
	if a := rowOf(r.view(sid, file), keyA); a.Checked || !a.Locked || a.Why != "archived" {
		t.Fatalf("archived own record row: %+v", a)
	}
	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA}); err != nil {
		t.Fatal(err)
	}
	if len(listRecords(r.placeDir(keyA))) != 1 {
		t.Fatal("a second record went next to the app's archived one")
	}
}

// Deleted in the desktop app: unticked, not written again by baton, and
// listed again only when ticked again, which clears what the delete left.
func TestDeletedInTheApp(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	must(t, os.Remove(rec))
	tomb := filepath.Join(r.placeDir(keyB), "deleted_"+sid)
	must(t, os.WriteFile(tomb, []byte("1800000005000"), 0o600))
	marker := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".desktop-released.json")
	must(t, os.WriteFile(marker, []byte(`{"v":1,"reason":"delete"}`), 0o600))
	must(t, r.d.Reconcile())
	if isFile(rec) {
		t.Fatal("a record the user deleted was written again")
	}
	if b := rowOf(r.view(sid, file), keyB); b.Checked || b.Why != "deleted" || b.Disabled {
		t.Fatalf("row after a delete in the app: %+v", b)
	}
	r.apply(sid, file, keyB)
	if !isFile(rec) || isFile(tomb) || isFile(marker) {
		t.Fatal("ticking again did not list it again and clear the delete's marks")
	}

	// A record that vanished without a delete is written again, when safe.
	must(t, os.Remove(rec))
	must(t, r.d.Reconcile())
	if !isFile(rec) {
		t.Fatal("a record that went missing was not written again")
	}
}

// After /clear the thread moves on: each record keeps its file and id and
// points to the new conversation once that has a file, with the old id in
// priorCliSessionIds and the desktop app's own fields kept; the old second
// name goes, the new one comes.
func TestFollowRepoints(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	m := r.record(keyB, "local_"+sid)
	m["lastFocusedAt"] = json.Number("1800000001234")
	m["permissionMode"] = "acceptEdits"
	b, _ := json.Marshal(m)
	must(t, os.WriteFile(rec, b, 0o600))

	next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
	must(t, r.d.Follow(sid, sid2, next)) // no file yet: the records wait
	if got := r.record(keyB, "local_"+sid)["cliSessionId"]; got != sid {
		t.Fatalf("repointed before the new conversation had a file: %v", got)
	}
	r.conversation(r.prof, sid2)
	must(t, r.d.Touch(sid2, next))
	for _, k := range []string{keyA, keyB} {
		m := r.record(k, "local_"+sid)
		prior, _ := m["priorCliSessionIds"].([]any)
		if m["cliSessionId"] != sid2 || m["sessionId"] != "local_"+sid || len(prior) != 1 || prior[0] != sid {
			t.Fatalf("record in %s: %v", k, m)
		}
	}
	if m := r.record(keyB, "local_"+sid); m["lastFocusedAt"] != 1800000001234.0 || m["permissionMode"] != "acceptEdits" {
		t.Fatalf("the app's own fields were not kept: %v", m)
	}
	dir := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)))
	if !sameFile(filepath.Join(dir, sid2+".jsonl"), next) || isFile(filepath.Join(dir, sid+".jsonl")) {
		t.Fatal("second names: the new one is missing or the old one stayed")
	}
	if ix := r.index(); len(ix.Threads) != 1 || ix.Threads[0].Session != sid2 || !slices.Equal(ix.Threads[0].Prior, []string{sid}) {
		t.Fatalf("thread: %+v", ix.Threads)
	}
	if v := r.view(sid2, next); !rowOf(v, keyA).Checked || !rowOf(v, keyB).Checked {
		t.Fatal("the choice did not follow the session")
	}
}

// The desktop app opened baton's record (so the session went on in a
// copy): in the list of the account the app is signed in to, the app keeps
// that record and the original conversation, and the copy gets a record of
// its own beside it, "<title> (baton)". In other lists the record follows
// the copy.
func TestFollowAfterTheAppOpenedIt(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	r.live(sid, "local_"+sid) // the app runs the record of its list (A's: it is signed in to A)
	before, _ := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json"))
	copyFile := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, copyFile))
	if after, _ := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json")); string(after) != string(before) {
		t.Fatal("the record the app has open was changed")
	}
	m := r.record(keyA, "local_"+sid2)
	if m == nil || m["cliSessionId"] != sid2 || m["title"] != "fix the <login> bug (baton)" {
		t.Fatalf("the copy's own record: %v", m)
	}
	if m := r.record(keyB, "local_"+sid); m["cliSessionId"] != sid2 || r.record(keyB, "local_"+sid2) != nil {
		t.Fatalf("B's record did not follow the copy: %v", m)
	}
	old := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	if !sameFile(old, file) {
		t.Fatal("the original's second name went while the app needs it")
	}
	v := r.view(sid2, copyFile)
	if a := rowOf(v, keyA); !a.Checked || !a.Copy || a.Locked {
		t.Fatalf("row of the list the app keeps the original in: %+v", a)
	}
	// Unticking the copy leaves the app's original alone.
	r.apply(sid2, copyFile)
	if after, _ := os.ReadFile(filepath.Join(r.placeDir(keyA), "local_"+sid+".json")); string(after) != string(before) {
		t.Fatal("unticking the copy touched the app's record")
	}
	if !sameFile(old, file) || r.record(keyA, "local_"+sid2) != nil {
		t.Fatal("the original's second name went while the app's record needs it, or the copy's record stayed")
	}
}

// The desktop app went on with baton's record in a conversation of its own
// (cleared it there): the record is the app's now, ticked and locked.
func TestTakenOverInTheApp(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	m := r.record(keyB, "local_"+sid)
	m["cliSessionId"] = "f0000000-0000-4000-8000-00000000000f"
	b, _ := json.Marshal(m)
	must(t, os.WriteFile(rec, b, 0o600))
	must(t, r.d.Reconcile())
	if row := rowOf(r.view(sid, file), keyB); !row.Checked || !row.Locked || row.Why != "released" {
		t.Fatalf("row: %+v", row)
	}
	r.apply(sid, file)
	if got, _ := os.ReadFile(rec); string(got) != string(b) {
		t.Fatal("a record the app took over was removed or changed")
	}
}

// While the desktop app runs, the list of the account it is signed in to is
// in its memory: baton changes it only when the user asks, and its own
// tidying waits until the app is closed (another account's list it does at
// once).
func TestRunningAppActiveList(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.running = true
	res := r.apply(sid, file, keyA, keyB)
	if !res.AppRunning || !res.ActiveAdded || !res.OtherAdded {
		t.Fatalf("result: %+v", res)
	}
	recA := filepath.Join(r.placeDir(keyA), "local_"+sid+".json")
	recB := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	before, _ := os.ReadFile(recA)
	// The conversation moves on: only B's record is brought up to date.
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-02T09:00:00.000Z","message":{"role":"assistant","content":"more"}}` + "\n")
	f.Close()
	must(t, r.d.Reconcile())
	if after, _ := os.ReadFile(recA); string(after) != string(before) {
		t.Fatal("the running app's own list was changed in the background")
	}
	if m := r.record(keyB, "local_"+sid); m["lastActivityAt"] != float64(1793610000000) {
		t.Fatalf("B's record not brought up to date: %v", m["lastActivityAt"])
	}
	// Unticked: removed at once; the app writes it back from memory, and
	// baton removes it again once the app is closed.
	res = r.apply(sid, file, keyB)
	if !res.ActiveRemoved || isFile(recA) {
		t.Fatalf("result: %+v", res)
	}
	must(t, os.WriteFile(recA, before, 0o600))
	must(t, r.d.Reconcile())
	if !isFile(recA) {
		t.Fatal("removed from the running app's list in the background")
	}
	r.running = false
	must(t, r.d.Reconcile())
	if isFile(recA) || r.index().Threads[0].Places[keyA] != nil || !isFile(recB) {
		t.Fatal("the record written back was not removed once the app closed")
	}
}

// The two names split (one side replaced the file): the thread is marked,
// nothing new can be ticked, its records are left alone, and nothing is
// mended by itself.
func TestSplit(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	second := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	tmp := file + ".compact.tmp"
	must(t, os.WriteFile(tmp, []byte(`{"type":"summary"}`+"\n"), 0o600))
	must(t, os.Rename(tmp, file))
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	before, _ := os.ReadFile(rec)
	must(t, r.d.Reconcile())
	if !r.index().Threads[0].Split || !r.index().Badges()[sid].Split {
		t.Fatal("split not noticed")
	}
	v := r.view(sid, file)
	if !v.Split || !rowOf(v, keyA).Disabled || !rowOf(v, keyB).Checked {
		t.Fatalf("view: %+v", v)
	}
	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA, keyB}); err == nil || err.(*Error).Code != "split" {
		t.Fatalf("ticked while split: %v", err)
	}
	if after, _ := os.ReadFile(rec); string(after) != string(before) || !isFile(second) || sameFile(second, file) {
		t.Fatal("a split thread was changed or mended")
	}
	r.apply(sid, file) // unticking still works; the second name (now a file of its own) stays
	if isFile(rec) || !isFile(second) {
		t.Fatal("unticking a split thread")
	}
}

// The second name is removed only while no Claude Code has the
// conversation open from the user's folder, and never when it is the only
// name left.
func TestSecondNameRemovedOnlyWhenUnheld(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	second := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	entry := r.live(sid, "")
	r.apply(sid, file)
	if !sameFile(second, file) {
		t.Fatal("the second name went while the desktop app had the conversation open")
	}
	must(t, os.Remove(entry))
	must(t, r.d.Reconcile())
	if isFile(second) || !isFile(file) {
		t.Fatal("the second name stayed once nobody had it open")
	}

	// Claude Code's cleanup took baton's name: the second one is all that is left.
	r.apply(sid, file, keyB)
	must(t, os.Remove(file))
	must(t, r.d.Reconcile())
	r.apply(sid, second)
	if !isFile(second) {
		t.Fatal("the only name left was removed")
	}
}

// A different file under the second name: never replaced, and it says why.
func TestConflict(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	other := r.conversation(filepath.Join(r.root, "elsewhere"), sid)
	second := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	must(t, os.MkdirAll(filepath.Dir(second), 0o700))
	must(t, os.Rename(other, second))
	if v := r.view(sid, file); v.Problem != "conflict" || !rowOf(v, keyA).Disabled {
		t.Fatalf("view: %+v", v)
	}
	if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, []string{keyA}); err == nil || err.(*Error).Code != "conflict" {
		t.Fatalf("apply: %v", err)
	}
	if len(listRecords(r.placeDir(keyA))) != 0 || sameFile(second, file) {
		t.Fatal("something was written")
	}
	// A conversation in the user's own folder already needs no second name.
	if v := r.view(sid, second); v.Problem != "" {
		t.Fatalf("view: %+v", v)
	}
	r.apply(sid, second, keyA)
	if m := r.record(keyA, "local_"+sid); m == nil {
		t.Fatal("not shown")
	}
}

// Unknown places and conversations without a file are refused.
func TestApplyRefuses(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	for _, c := range []struct {
		keys []string
		code string
	}{
		{[]string{"../../x"}, "unknown_place"},
		{[]string{acctB + "/" + orgA}, "unknown_place"},
		{[]string{keyX}, "unknown_place"},
	} {
		if _, err := r.d.Apply(Conversation{ID: sid, Transcript: file}, c.keys); err == nil || err.(*Error).Code != c.code {
			t.Fatalf("%v: %v", c.keys, err)
		}
	}
	if _, err := r.d.Apply(Conversation{ID: sid2, Transcript: filepath.Join(r.prof, "none.jsonl")}, []string{keyA}); err == nil || err.(*Error).Code != "no_file" {
		t.Fatalf("no file: %v", err)
	}
	r.d.SetData("")
	if _, err := r.d.Places(Conversation{ID: sid, Transcript: file}); err == nil || err.(*Error).Code != "no_desktop" {
		t.Fatalf("no desktop app: %v", err)
	}
	if isFile(r.d.IndexPath) {
		t.Fatal("refusals wrote the index")
	}
}

// Following and looking at a thread cost nothing while baton shows nothing.
func TestNothingShownTouchesNothing(t *testing.T) {
	r := newRig(t)
	r.d.SetData(filepath.Join(r.root, "never"))
	must(t, r.d.Follow(sid, sid2, ""))
	must(t, r.d.Touch(sid, ""))
	must(t, r.d.Reconcile())
	if isFile(r.d.IndexPath) || isFile(r.d.LockPath) {
		t.Fatal("index or lock written")
	}
}

// Inside a test binary, a change outside the temporary folder panics.
func TestGuard(t *testing.T) {
	ok := func(p string) (panicked bool) {
		defer func() { panicked = recover() != nil }()
		guard("write", p)
		return false
	}
	if ok(filepath.Join(t.TempDir(), "x")) {
		t.Fatal("the temporary folder is refused")
	}
	// The same folder spelled in long form and in 8.3 form.
	if l := longPath(os.TempDir()); ok(filepath.Join(l, "y")) {
		t.Fatal("the temporary folder in long form is refused")
	}
	for _, p := range []string{`C:\Windows\baton-test`, filepath.Join(os.Getenv("LOCALAPPDATA"), "Claude-Data", "x"),
		filepath.Join(os.TempDir()+"x", "y"), filepath.Join(os.TempDir(), "..", "z")} {
		if !ok(p) {
			t.Fatalf("%s was not refused", p)
		}
	}
	if !func() (p bool) {
		defer func() { p = recover() != nil }()
		_ = removeFile(`C:\Windows\baton-test`)
		return
	}() {
		t.Fatal("removeFile outside the temporary folder did not panic")
	}
	// The user's own folders are refused by name too, whatever TMP says.
	t.Setenv("TMP", filepath.Join(os.Getenv("LOCALAPPDATA"), "Claude-Data"))
	if !ok(filepath.Join(os.Getenv("LOCALAPPDATA"), "Claude-Data", "x")) {
		t.Fatal("the desktop app's real data folder was not refused with TMP set to it")
	}
}

func TestFindDataDir(t *testing.T) {
	root := t.TempDir()
	la, ad := filepath.Join(root, "local"), filepath.Join(root, "roaming")
	if findDataDir(la, ad) != "" {
		t.Fatal("found one where there is none")
	}
	old := filepath.Join(ad, "Claude")
	must(t, os.MkdirAll(filepath.Join(old, sessionsDir), 0o700))
	if findDataDir(la, ad) != old {
		t.Fatal("the older %APPDATA% folder")
	}
	must(t, os.WriteFile(filepath.Join(old, ".user-data-left.claude_x"), nil, 0o600))
	if findDataDir(la, ad) != "" {
		t.Fatal("a folder the app moved away from")
	}
	a, b := filepath.Join(la, "Claude-Data", "claude_a"), filepath.Join(la, "Claude-Data", "claude_b")
	must(t, os.MkdirAll(a, 0o700))
	must(t, os.WriteFile(filepath.Join(a, "config.json"), []byte("{}"), 0o600))
	must(t, os.MkdirAll(filepath.Join(b, sessionsDir), 0o700))
	must(t, os.MkdirAll(filepath.Join(la, "Claude-Data", "claude-code"), 0o700))
	if got := findDataDir(la, ad); got != b {
		t.Fatalf("got %s, want the one with session lists", got)
	}
	t.Setenv("BATON_DESKTOP_DATA", a)
	if DataDir() != a {
		t.Fatal("BATON_DESKTOP_DATA")
	}
	t.Setenv("BATON_DESKTOP_DATA", "")
	if DataDir() != "" {
		t.Fatal("a test binary looked for the real data folder")
	}
}

func TestActive(t *testing.T) {
	dir := t.TempDir()
	if Active(dir) != "" {
		t.Fatal("no config.json")
	}
	must(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte("\xef\xbb\xbf"+`{"lastKnownAccountUuid":"`+strings.ToUpper(acctA)+`"}`), 0o600))
	if Active(dir) != acctA {
		t.Fatal(Active(dir))
	}
	must(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"lastKnownAccountUuid":"..\\x"}`), 0o600))
	if Active(dir) != "" {
		t.Fatal("an id that is not one")
	}
}

func TestAppImage(t *testing.T) {
	for p, want := range map[string]bool{
		`C:\Program Files\WindowsApps\Claude_1.2.3.0_x64__pzs8sxrjxfjjc\app\claude.exe`:                         true,
		`C:\Users\u\AppData\Local\AnthropicClaude\app-0.9.1\claude.exe`:                                         true,
		`C:\Users\u\AppData\Local\Claude-Data\claude_pzs8sxrjxfjjc\claude-code\2.1.317\abc\claude.exe`:          false,
		`C:\Users\u\.local\bin\claude.exe`:                                                                      false,
		`C:\Program Files\WindowsApps\Claude_1.2.3.0_x64__pzs8sxrjxfjjc\app\resources\claude-code\x\claude.exe`: false,
		`C:\Program Files\WindowsApps\Claude_1.2.3.0_x64__pzs8sxrjxfjjc\app\claude-helper.exe`:                  false,
	} {
		if IsAppImage(p) != want {
			t.Errorf("%s: want %v", p, want)
		}
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	if !AppRunning() {
		t.Fatal("BATON_DESKTOP_RUNNING=1")
	}
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	if AppRunning() {
		t.Fatal("BATON_DESKTOP_RUNNING=0")
	}
}

// A record baton has not written yet (the copy's, waiting for its file)
// never takes, changes or removes a file of the app's that has its name.
func TestPendingRecordNeverTakesAnotherFile(t *testing.T) {
	for _, untick := range []bool{false, true} {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.live(sid, "local_"+sid)
		next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
		must(t, r.d.Follow(sid, sid2, next)) // no file yet: the copy's record waits
		theirs := filepath.Join(r.placeDir(keyA), "local_"+sid2+".json")
		content := `{"sessionId":"local_` + sid2 + `","cliSessionId":"f0000000-0000-4000-8000-00000000000f","cwd":"x"}`
		must(t, os.WriteFile(theirs, []byte(content), 0o600))
		if untick {
			if _, err := r.d.Apply(Conversation{ID: sid2, Transcript: next}, nil); err != nil {
				t.Fatal(err)
			}
		}
		r.conversation(r.prof, sid2)
		must(t, r.d.Touch(sid2, next))
		if b, _ := os.ReadFile(theirs); string(b) != content {
			t.Fatalf("untick=%v: the app's file was changed or removed: %s", untick, b)
		}
		n := 0
		for _, path := range listRecords(r.placeDir(keyA)) {
			if h, ok := readHead(path); ok && h.Cli == sid2 {
				n++
			}
		}
		if want := map[bool]int{false: 1, true: 0}[untick]; n != want {
			t.Fatalf("untick=%v: %d records of the copy, want %d", untick, n, want)
		}
	}
}

// A list baton shows the session in stays on offer after its account was
// removed from baton, so it can still be unticked.
func TestRemovedAccountStaysOnOffer(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	r.d.Accounts = slices.DeleteFunc(r.d.Accounts, func(a accounts.Account) bool { return a.ID == "b" })
	v := r.view(sid, file)
	b := rowOf(v, keyB)
	if !b.Former || !b.Checked || b.Disabled || !slices.Equal(b.Accounts, []string{"b"}) || v.Rows[1].Key != keyB {
		t.Fatalf("row of a removed account: %+v in %+v", b, v.Rows)
	}
	res := r.apply(sid, file)
	if !slices.Equal(res.Removed, []string{keyB}) || r.record(keyB, "local_"+sid) != nil {
		t.Fatalf("unticking it: %+v", res)
	}
	if rowOf(r.view(sid, file), keyB).Key != "" {
		t.Fatal("still on offer once unticked")
	}
}
