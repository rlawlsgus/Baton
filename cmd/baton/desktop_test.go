package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/claude"
	"baton/internal/config"
	"baton/internal/desklogin"
	"baton/internal/desktop"
	"baton/internal/fsx"
	"baton/internal/run"

	"golang.org/x/sys/windows"
)

// `baton desktop <id>` says where a conversation shows in the Claude
// desktop app and changes it, by account name. Every folder is a temporary
// one: the user's home, the desktop app's data folder and baton's.
func TestDesktopCommand(t *testing.T) {
	c := newCLI(t)
	root := t.TempDir()
	data := filepath.Join(root, "Claude-Data", "claude_test")
	home := filepath.Join(root, "user")
	env := []string{"USERPROFILE=" + home, "LOCALAPPDATA=" + filepath.Join(root, "local"), "APPDATA=" + filepath.Join(root, "roaming"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"), "BATON_DESKTOP_DATA=" + data, "BATON_DESKTOP_RUNNING=0"}
	if out, code := c.run(env, "add", "personal"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	a := c.registry().Get("personal")
	if a == nil || len(a.AccountUUID) != 36 || len(a.OrgID) != 36 {
		t.Fatalf("the account's ids were not read at sign-in: %+v", a)
	}
	place := filepath.Join(data, "claude-code-sessions", a.AccountUUID, a.OrgID)
	if err := os.MkdirAll(place, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.json"), []byte(`{"lastKnownAccountUuid":"`+a.AccountUUID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const sid = "d35c0000-0000-4000-8000-000000000001"
	proj := filepath.Join(root, "Proj")
	file := filepath.Join(a.ProfileDir, "projects", claude.ProjectSlug(proj), sid+".jsonl")
	_ = os.MkdirAll(proj, 0o700)
	_ = os.MkdirAll(filepath.Dir(file), 0o700)
	if err := os.WriteFile(file, []byte(`{"type":"user","cwd":`+strconv.Quote(proj)+`,"timestamp":"2026-11-01T10:00:00Z","message":{"role":"user","content":"tidy the docs"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(place, "local_"+sid+".json")
	second := filepath.Join(home, ".claude", "projects", claude.ProjectSlug(proj), sid+".jsonl")

	out, code := c.run(env, "desktop", sid)
	if code != 0 || !strings.Contains(out, "tidy the docs") || !strings.Contains(out, "[ ] personal (personal@example.com)") ||
		!strings.Contains(out, "signed in to the desktop app now") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	// While a delete of it runs (in the window, say), it is not listed
	// anywhere again; a look changes nothing, so it still works.
	release, err := run.MarkDeleting(c.home, sid)
	if err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(env, "desktop", sid, "--show", "personal"); code == 0 || !strings.Contains(out, "conversation "+sid+" is being deleted") ||
		isFile(record) || isFile(second) {
		t.Fatalf("show while it is being deleted: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", sid); code != 0 || !strings.Contains(out, "[ ] personal") {
		t.Fatalf("a look while it is being deleted: %d\n%s", code, out)
	}
	release()
	out, code = c.run(env, "desktop", sid, "--show", "personal")
	if code != 0 || !strings.Contains(out, "Done. It shows the next time you open the Claude desktop app.") || !isFile(record) || !isFile(second) {
		t.Fatalf("show: %d\n%s", code, out)
	}
	if out, _ := c.run(env, "desktop", sid); !strings.Contains(out, "[x] personal") {
		t.Fatalf("list after show:\n%s", out)
	}
	if out, code := c.run(env, "desktop", sid, "--show", "nobody"); code == 0 || !strings.Contains(out, `no account "nobody"`) {
		t.Fatalf("an unknown account: %d\n%s", code, out)
	}
	if out, code := c.run(append(env, "BATON_RUN=x"), "desktop", sid, "--hide", "personal"); code == 0 || !strings.Contains(out, "inside a baton session") || !isFile(record) {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", sid, "--hide", "personal")
	if code != 0 || isFile(record) || isFile(second) || !isFile(file) {
		t.Fatalf("hide: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "d35c0000-0000-4000-8000-000000000009"); code == 0 || !strings.Contains(out, "first message") {
		t.Fatalf("a conversation with no file: %d\n%s", code, out)
	}
}

// loginEnv is a PC of its own for switching the desktop app's sign-in: the
// user's folders and a fake desktop app's data folder, all temporary, the
// app quit, and not inside a Claude Code (the harness runs in one).
func loginEnv(t *testing.T) (env []string, data string) {
	t.Helper()
	t.Setenv("CLAUDECODE", "")
	root := t.TempDir()
	data = filepath.Join(root, "Claude-Data", "claude_test")
	user := filepath.Join(root, "user")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	return []string{"USERPROFILE=" + user, "CLAUDE_CONFIG_DIR=" + filepath.Join(user, ".claude"),
		"LOCALAPPDATA=" + filepath.Join(root, "local"), "APPDATA=" + filepath.Join(root, "roaming"),
		"BATON_DESKTOP_DATA=" + data, "BATON_DESKTOP_RUNNING=0", "OneDrive=", "OneDriveConsumer=", "OneDriveCommercial="}, data
}

// appSignIn makes the fake desktop app look signed in to acct, as the app
// leaves it: its sign-in files tagged tag, and the account's values among
// settings of the app's own in config.json.
func appSignIn(t *testing.T, data, acct, tag string) {
	t.Helper()
	for _, it := range []string{"Local Storage", "Session Storage", "IndexedDB", "WebStorage", filepath.Join("Network", "Cookies")} {
		if err := os.RemoveAll(filepath.Join(data, it)); err != nil {
			t.Fatal(err)
		}
	}
	for p, c := range map[string]string{
		filepath.Join("Local Storage", "leveldb", "000003.log"):                         "ls-" + tag,
		filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log"): "idb-" + tag,
		filepath.Join("Network", "Cookies"):                                             "cookies-" + tag,
		"config.json": "{\n  \"locale\": \"ko-KR\",\n  \"oauth:tokenCache\": \"sealed-" + tag + "\",\n  \"oauth:tokenCacheV2\": \"sealed2-" + tag +
			"\",\n  \"lastKnownAccountUuid\": \"" + acct + "\",\n  \"windowSizeWasSignedIn\": true\n}",
	} {
		p = filepath.Join(data, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// oneLine is text with every run of spaces and line breaks made one space,
// so a sentence can be found whatever line it is wrapped on.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// envValue is the value of key in env, the last one given.
func envValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if s, ok := strings.CutPrefix(kv, key+"="); ok {
			v = s
		}
	}
	return v
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// cutOffStage leaves in the fake app's folder the stage of a switch from
// account from that was cut off before it moved anything, as a switch
// leaves it: its journal, and config.json as it was.
func cutOffStage(t *testing.T, data, from string) string {
	t.Helper()
	stage := filepath.Join(data, ".baton-switch-0123456789ab")
	for p, s := range map[string]string{
		filepath.Join(stage, "journal.json"):       `{"schema":1,"from":"` + from + `","to":"","phase":"moving","cfg_before":"x","items":[]}`,
		filepath.Join(stage, "old", "config.json"): readFile(t, filepath.Join(data, "config.json")),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return stage
}

// setDesktopLogins turns switching the desktop app's sign-in on or off in
// baton's folder home, as accepting its notice in the window does.
func setDesktopLogins(t *testing.T, home string, on bool, ack int) {
	t.Helper()
	if _, err := config.Update(filepath.Join(home, "config.json"), filepath.Join(home, "config.lock"), func(c *config.Config) error {
		c.DesktopLogins, c.DesktopLoginsAcknowledged = on, ack
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// `baton desktop login` lists the sign-ins baton keeps for the desktop app
// and switches between them, only when turned on, never from inside a
// session or a Claude Code, and only while the app is quit.
func TestDesktopLoginCommand(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	if out, code := c.run(env, "add", "work"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	work := c.registry().Get("work").AccountUUID
	const other = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	appSignIn(t, data, work, "W")
	store := filepath.Join(c.home, "desktop-logins")

	if out, code := c.run(env, "desktop", "login"); code != 0 || !strings.Contains(out, "sign-in is off") {
		t.Fatalf("list while off: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "use", "work"); code == 0 || !strings.Contains(out, "turned off") {
		t.Fatalf("use while off: %d\n%s", code, out)
	}
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)

	out, code := c.run(env, "desktop", "login")
	if code != 0 || !strings.Contains(out, "* work") || !strings.Contains(out, "signed in now") || !strings.Contains(out, "not kept yet") ||
		!strings.Contains(out, "Log out") {
		t.Fatalf("list: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", "login", "add")
	if code != 0 || !strings.Contains(out, "baton kept the sign-in of work") || !strings.Contains(out, "Do not use Log out") {
		t.Fatalf("add: %d\n%s", code, out)
	}
	if strings.Contains(readFile(t, filepath.Join(data, "config.json")), "lastKnownAccountUuid") || isFile(filepath.Join(data, "Network", "Cookies")) ||
		readFile(t, filepath.Join(store, work, "Network", "Cookies")) != "cookies-W" {
		t.Fatal("add did not keep the sign-in and empty the app's")
	}
	if out, code := c.run(env, "desktop", "login", "add"); code == 0 || !strings.Contains(out, "signed out on this PC already") {
		t.Fatalf("add again: %d\n%s", code, out)
	}

	// The user signs in to another account in the app, and switches back.
	appSignIn(t, data, other, "O")
	if out, code := c.run(env, "desktop", "login"); !strings.Contains(out, "* bbbbbbbb") || !strings.Contains(out, "not one of baton's accounts") {
		t.Fatalf("list after a sign-in: %d\n%s", code, out)
	}
	if out, code := c.run(append(env, "BATON_LANG=ko"), "desktop", "login"); code != 0 || !strings.Contains(out, "지금 로그인됨; baton 계정 아님") {
		t.Fatalf("list in Korean: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", "login", "use", "work", "--open")
	if code != 0 || !strings.Contains(out, "The desktop app is now on work.") || !strings.Contains(out, "baton kept the sign-in the app had (bbbbbbbb)") {
		t.Fatalf("use work: %d\n%s", code, out)
	}
	if !strings.Contains(readFile(t, filepath.Join(data, "config.json")), `"lastKnownAccountUuid": "`+work+`"`) ||
		readFile(t, filepath.Join(data, "Network", "Cookies")) != "cookies-W" || readFile(t, filepath.Join(store, other, "Network", "Cookies")) != "cookies-O" {
		t.Fatal("use did not put work's sign-in back and keep the other one")
	}
	if out, code := c.run(env, "desktop", "login", "label", "bbbbbbbb", "My", "laptop"); code != 0 || !strings.Contains(out, "It is called My laptop now.") {
		t.Fatalf("label: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "label", "work", "deadbeef"); code == 0 || !strings.Contains(out, "look like an account id") {
		t.Fatalf("a label like an id: %d\n%s", code, out)
	}

	// Never from inside a session or a Claude Code, nor while the app runs.
	cfg := readFile(t, filepath.Join(data, "config.json"))
	for _, c2 := range []struct {
		env  string
		want string
	}{
		{"BATON_RUN=r1", "inside a baton session"},
		{"CLAUDECODE=1", "inside Claude Code"},
		{"BATON_DESKTOP_RUNNING=1", "still running"},
	} {
		if out, code := c.run(append(env, c2.env), "desktop", "login", "use", "my laptop"); code == 0 || !strings.Contains(out, c2.want) {
			t.Errorf("%s: %d\n%s", c2.env, code, out)
		}
	}
	if readFile(t, filepath.Join(data, "config.json")) != cfg {
		t.Fatal("a refused switch changed the app")
	}
	// In Korean too it says to run it elsewhere, never to change it in the
	// app (whose Log out would end the sign-ins kept).
	if out, _ := c.run(append(env, "CLAUDECODE=1", "BATON_LANG=ko"), "desktop", "login", "use", "my laptop"); !strings.Contains(out, "다른 터미널에서 직접 실행하세요") ||
		strings.Contains(out, "직접 바꾸세요") {
		t.Errorf("inside Claude Code, in Korean:\n%s", out)
	}
	if out, code := c.run(append(env, "BATON_LANG=ko"), "desktop", "login", "use", "nobody"); code == 0 || !strings.Contains(out, `"nobody" 계정이 없습니다`) {
		t.Fatalf("an unknown name in Korean: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "recover"); code != 0 || !strings.Contains(out, "nothing to put back") {
		t.Fatalf("recover: %d\n%s", code, out)
	}

	// Forgetting asks first; without a console it says why.
	if out, code := c.run(env, "desktop", "login", "forget", "my laptop"); code == 0 || !strings.Contains(out, "not a console") ||
		!strings.Contains(out, "does not sign the account out on Claude's servers") || !isFile(filepath.Join(store, other, "meta.json")) {
		t.Fatalf("forget without a console: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "my laptop", "--yes"); code != 0 || isFile(filepath.Join(store, other, "meta.json")) {
		t.Fatalf("forget: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "--all"); code == 0 || !strings.Contains(out, "not a console") {
		t.Fatalf("forget --all without a console: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "--all", "--yes"); code != 0 || !strings.Contains(out, "Deleted what baton kept: 1 sign-in(s).") {
		t.Fatalf("forget --all: %d\n%s", code, out)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("the store is still there: %v", err)
	}
	if !strings.Contains(readFile(t, filepath.Join(data, "config.json")), `"lastKnownAccountUuid": "`+work+`"`) {
		t.Fatal("forgetting changed the app")
	}
	if out, code := c.run(env, "desktop", "login", "work"); code == 0 || !strings.Contains(out, "usage:") {
		t.Fatalf("no such subcommand: %d\n%s", code, out)
	}

	// A switch that was cut off is listed, and put back on request.
	stage := cutOffStage(t, data, work)
	if out, code := c.run(env, "desktop", "login"); code != 0 || !strings.Contains(out, "baton desktop login recover") {
		t.Fatalf("list with a cut-off switch: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "recover"); code != 0 || !strings.Contains(out, "was undone: the desktop app is back on work") {
		t.Fatalf("recover: %d\n%s", code, out)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the stage is still there: %v", err)
	}
}

// Forgetting or naming the sign-in of an account baton keeps none for (one
// of baton's accounts, or the one the app is on but not kept yet) says so at
// once: it neither asks to delete a sign-in that is not there nor gives
// advice about switching. A text that names baton first is printed without
// "baton: " before it, which would read "baton: baton keeps…".
func TestDesktopLoginNothingKept(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	if out, code := c.run(env, "add", "work"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	const other = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	appSignIn(t, data, other, "O")
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	for _, name := range []string{"work", "bbbbbbbb"} {
		out, code := c.run(env, "desktop", "login", "forget", name, "--yes")
		if code == 0 || !strings.Contains(out, "baton keeps no sign-in for "+name+"; there is nothing to forget") ||
			strings.Contains(out, "baton deletes") || strings.Contains(out, "switch to it") || strings.Contains(out, "baton: baton") {
			t.Errorf("forget %s: %d\n%s", name, code, out)
		}
		out, code = c.run(env, "desktop", "login", "label", name, "Home")
		if code == 0 || !strings.Contains(out, "baton keeps no sign-in for "+name+" yet; it can be named once baton keeps it") ||
			strings.Contains(out, "switch to it") || strings.Contains(out, "baton: baton") {
			t.Errorf("label %s: %d\n%s", name, code, out)
		}
	}
	if out, code := c.run(env, "desktop", "login", "use", "work"); code == 0 || !strings.Contains(out, "baton keeps no sign-in for that account") ||
		strings.Contains(out, "baton: baton") {
		t.Errorf("use work: %d\n%s", code, out)
	}
	if out, _ := c.run(append(env, "BATON_LANG=ko"), "desktop", "login", "forget", "work"); !strings.Contains(out, "baton이 보관한 work의 로그인이 없어 지울 것이 없습니다") ||
		strings.Contains(out, "baton: baton") {
		t.Errorf("forget in Korean:\n%s", out)
	}
	// Any other text still has it.
	if out, _ := c.run(append(env, "BATON_LANG=ko"), "desktop", "login", "label", "work", "Home"); !strings.Contains(out, "baton: 아직 work의 로그인을 보관하지 않아") {
		t.Errorf("label in Korean:\n%s", out)
	}
	// Once kept, the same name is forgotten.
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "bbbbbbbb", "--yes"); code != 0 || !strings.Contains(out, "Deleted.") {
		t.Fatalf("forget a kept one: %d\n%s", code, out)
	}
}

// What a signed-out desktop app left is no sign-in: turning switching off
// and forgetting everything count it apart from the sign-ins, and never say
// the app stays signed in when it is signed out.
func TestDesktopLoginLeftoversOnly(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	appSignIn(t, data, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	// Opened and quit without a sign-in, the app leaves cookies of its own;
	// adding again sets them aside. Then the one sign-in kept is forgotten.
	if err := os.MkdirAll(filepath.Join(data, "Network"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "Network", "Cookies"), []byte("cookies-none"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 || !strings.Contains(out, "set aside as unnamed-") {
		t.Fatalf("add again: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "aaaaaaaa", "--yes"); code != 0 {
		t.Fatalf("forget: %d\n%s", code, out)
	}

	const left = "1 item(s) a signed-out desktop app left (no token recorded)"
	out, code := c.run(env, "config", "desktop_logins", "false")
	if code == 0 || !strings.Contains(out, "Turning this off deletes what baton keeps for the Claude desktop app: "+left+".") ||
		strings.Contains(out, "sign-in(s)") || !strings.Contains(out, "signed out now") || strings.Contains(out, "stays signed in") {
		t.Fatalf("off: %d\n%s", code, out)
	}
	out, code = c.run(env, "desktop", "login", "forget", "--all")
	if code == 0 || !strings.Contains(out, "baton deletes everything it keeps for the Claude desktop app: "+left+".") ||
		strings.Contains(out, "sign-in(s)") || strings.Contains(out, "stays signed in") {
		t.Fatalf("forget --all: %d\n%s", code, out)
	}
	if out, _ := c.run(append(env, "BATON_LANG=ko"), "desktop", "login", "forget", "--all"); !strings.Contains(out, "모두 지웁니다: 로그아웃 상태의 앱이 남긴 것 1개(토큰 기록 없음).") ||
		strings.Contains(out, "로그인 0개") {
		t.Fatalf("forget --all in Korean:\n%s", out)
	}
	// Turned off with it still there, the list counts it apart too.
	setDesktopLogins(t, c.home, false, config.DesktopLoginsDisclosure)
	if out, _ := c.run(env, "desktop", "login"); !strings.Contains(out, "baton still keeps this for the desktop app from before: "+left) {
		t.Fatalf("list while off:\n%s", out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "--all", "--yes"); code != 0 || !strings.Contains(out, "Deleted what baton kept: "+left+".") {
		t.Fatalf("forget --all --yes: %d\n%s", code, out)
	}
}

// A sign-in is called by its label, else by the first of baton's accounts
// that is that Claude account (as the list names it), else by the start of
// its id.
func TestLabelOf(t *testing.T) {
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	accts := []accounts.Account{{ID: "spare", AccountUUID: acct, Priority: 1}, {ID: "work", AccountUUID: strings.ToUpper(acct), Priority: 5},
		{ID: "home", AccountUUID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Priority: 9}}
	for _, c := range []struct {
		v     desklogin.View
		accts []accounts.Account
		want  string
	}{
		{desklogin.View{Logins: []desklogin.Login{{Account: acct, Label: "Laptop"}}}, accts, "Laptop"},
		{desklogin.View{}, accts, "work"},
		{desklogin.View{}, nil, "aaaaaaaa"},
	} {
		if got := labelOf(c.v, c.accts, acct); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// baton config desktop_logins: on shows its notice and needs it accepted at
// a console; off asks before it deletes the sign-ins kept.
func TestConfigDesktopLogins(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	on := func() bool {
		t.Helper()
		cfg, err := config.Load(filepath.Join(c.home, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		return cfg.DesktopLoginsOn()
	}
	out, code := c.run(env, "config", "desktop_logins", "true")
	if code == 0 || !strings.Contains(out, "Before baton switches the Claude desktop app's sign-in") || !strings.Contains(out, "not a console") ||
		!strings.Contains(out, filepath.Join(c.home, "desktop-logins")) || on() {
		t.Fatalf("on without a console: %d\n%s", code, out)
	}
	// It names the window's button for adding an account to the desktop
	// app, never baton's own "Add account" (a baton account).
	if !strings.Contains(oneLine(out), `use "Add an account to the desktop app" on baton's Accounts tab (or: baton desktop login add)`) {
		t.Fatalf("the notice's button:\n%s", out)
	}
	if out, code := c.run(append(env, "BATON_LANG=ko"), "config", "desktop_logins", "true"); code == 0 || !strings.Contains(out, "꼭 읽어 주세요") ||
		!strings.Contains(oneLine(out), "baton 계정 탭의 ‘데스크톱 앱에 계정 추가’를 쓰세요(또는 baton desktop login add)") {
		t.Fatalf("the notice in Korean: %d\n%s", code, out)
	}
	if out, code := c.run(env, "config", "desktop_logins", "maybe"); code == 0 || !strings.Contains(out, "true or false") {
		t.Fatalf("maybe: %d\n%s", code, out)
	}
	if out, code := c.run(append(env, "BATON_RUN=r1"), "config", "desktop_logins", "true"); code == 0 || !strings.Contains(out, "inside a baton session") {
		t.Fatalf("inside a session: %d\n%s", code, out)
	}
	// The notice accepted before (as it reads now): on without a question.
	setDesktopLogins(t, c.home, false, config.DesktopLoginsDisclosure)
	if out, code := c.run(env, "config", "desktop_logins", "true"); code != 0 || !on() {
		t.Fatalf("on again: %d\n%s", code, out)
	}
	// A sign-in kept: off asks before deleting it.
	appSignIn(t, data, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	// The app is signed out now (add emptied it): never said to stay
	// signed in. Without a console, the way round the question is named.
	if out, code := c.run(env, "config", "desktop_logins", "false"); code == 0 || !strings.Contains(out, "deletes what baton keeps for the Claude desktop app: 1 sign-in(s).") ||
		!strings.Contains(out, "signed out now") || strings.Contains(out, "stays signed in") ||
		!strings.Contains(out, "(or first: baton desktop login forget --all --yes)") || !on() {
		t.Fatalf("off without a console: %d\n%s", code, out)
	}
	if out, code := c.run(env, "desktop", "login", "forget", "--all", "--yes"); code != 0 {
		t.Fatalf("forget --all: %d\n%s", code, out)
	}
	// Nothing kept: off needs no question, and the notice shows again next time.
	if out, code := c.run(env, "config", "desktop_logins", "false"); code != 0 || on() || !strings.Contains(out, "signed out now") || strings.Contains(out, "stays signed in") ||
		strings.Contains(out, "Deleted") {
		t.Fatalf("off: %d\n%s", code, out)
	}
	if out, _ := c.run(env, "config", "desktop_logins"); strings.TrimSpace(out) != "false" {
		t.Fatalf("get: %s", out)
	}
	if out, code := c.run(env, "config", "desktop_logins", "true"); code == 0 || !strings.Contains(out, "not a console") {
		t.Fatalf("on after off asks again: %d\n%s", code, out)
	}
}

// Uninstalling deletes the sign-ins kept for the desktop app, also when
// baton's folder is not named baton (it is left then), after it puts back a
// switch that was cut off; while the app runs, that cannot be done, and
// uninstalling is refused.
func TestUninstallDesktopLogins(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	appSignIn(t, data, acct, "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	appSignIn(t, data, acct, "A")
	cfg := readFile(t, filepath.Join(data, "config.json"))
	stage := cutOffStage(t, data, acct)
	store := filepath.Join(c.home, "desktop-logins")
	if strings.EqualFold(filepath.Base(c.home), "baton") {
		t.Fatal("setup: baton's folder must not be named baton here")
	}

	out, code := c.run(append(env, "BATON_DESKTOP_RUNNING=1"), "uninstall", "--yes")
	if code == 0 || !strings.Contains(out, "was cut off") || !isFile(filepath.Join(store, acct, "meta.json")) || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatalf("uninstall while the app runs: %d\n%s", code, out)
	}
	// Nor from inside a Claude Code, where the switch cannot be put back:
	// refused before the question, with every conversation where it was.
	if out, code := c.run(env, "add", "a"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	const sid = "11111111-2222-4333-8444-0000000000c1"
	conv := filepath.Join(c.registry().Get("a").ProfileDir, "projects", "C--proj", sid+".jsonl")
	moved := filepath.Join(envValue(env, "CLAUDE_CONFIG_DIR"), "projects", "C--proj", sid+".jsonl")
	_ = os.MkdirAll(filepath.Dir(conv), 0o700)
	if err := os.WriteFile(conv, []byte(`{"type":"user","cwd":"C:\proj"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = c.run(append(env, "CLAUDECODE=1"), "uninstall", "--yes")
	if code == 0 || !strings.Contains(out, "only when run outside Claude Code") || strings.Contains(out, "moved 1 conversation") ||
		!isFile(conv) || isFile(moved) || !isFile(filepath.Join(store, acct, "meta.json")) || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatalf("uninstall inside a Claude Code: %d\n%s", code, out)
	}
	if out, _ := c.run(append(env, "CLAUDECODE=1", "BATON_LANG=ko"), "uninstall", "--yes"); !strings.Contains(out, "다른 터미널에서 baton uninstall을 실행하세요") {
		t.Fatalf("uninstall inside a Claude Code, in Korean:\n%s", out)
	}
	out, code = c.run(env, "uninstall", "--yes")
	if code != 0 || !strings.Contains(out, "It also deletes what baton keeps for the Claude desktop app: 1 sign-in(s).") ||
		!strings.Contains(out, "The desktop app stays signed in to aaaaaaaa.") || !strings.Contains(out, "deleted the 1 sign-in(s) kept") ||
		!strings.Contains(out, "not named baton") || !strings.Contains(out, "moved 1 conversation") {
		t.Fatalf("uninstall: %d\n%s", code, out)
	}
	// The folder is left, and with it config.json: switching is off there,
	// so installing baton on it again shows the notice again.
	cfgAfter, err := config.Load(filepath.Join(c.home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfgAfter.DesktopLogins || cfgAfter.DesktopLoginsAcknowledged != 0 {
		t.Fatalf("switching is still on in the folder left: %v %d", cfgAfter.DesktopLogins, cfgAfter.DesktopLoginsAcknowledged)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("the sign-ins kept are still there: %v", err)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the cut-off switch is still there: %v", err)
	}
	if readFile(t, filepath.Join(data, "config.json")) != cfg || readFile(t, filepath.Join(data, "Network", "Cookies")) != "cookies-A" {
		t.Fatal("the app's sign-in changed")
	}
	if _, err := os.Stat(c.home); err != nil {
		t.Fatalf("a folder not named baton was deleted: %v", err)
	}
}

// The message after a change says when each account's list shows it.
func TestDesktopDoneLine(t *testing.T) {
	t.Setenv("BATON_LANG", "")
	for _, c := range []struct {
		r    desktop.Result
		want string
	}{
		{desktop.Result{Added: []string{"x"}, OtherAdded: true}, "Done. It shows when you switch to that account in the desktop app."},
		{desktop.Result{Added: []string{"x"}, ActiveAdded: true}, "Done. It shows the next time you open the Claude desktop app."},
		{desktop.Result{Added: []string{"x", "y"}, ActiveAdded: true, OtherAdded: true, AppRunning: true},
			"Done. Quit the Claude desktop app (also from its tray icon) and open it again to see the change. It shows when you switch to that account in the desktop app."},
		{desktop.Result{Removed: []string{"x"}, ActiveRemoved: true, AppRunning: true},
			"Done. It disappears from the desktop app once you quit the app while baton's window is open."},
		{desktop.Result{Removed: []string{"x"}}, "Done. It is no longer listed for those accounts."},
		{desktop.Result{}, "Nothing changed."},
	} {
		if got := doneLine(c.r); got != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c.r, got, c.want)
		}
	}
	t.Setenv("BATON_LANG", "ko")
	r := desktop.View{Rows: []desktop.Row{{Accounts: []string{"laptop", "work"}, Key: "k"}}}
	if got := placeNote(r, r.Rows[0]); !strings.Contains(got, "laptop, work은(는) 같은 Claude 계정입니다") {
		t.Errorf("korean: %s", got)
	}
	if got := placeNote(r, desktop.Row{Why: "cloud", Provider: "bedrock"}); !strings.Contains(got, "Amazon Bedrock을(를)") {
		t.Errorf("provider: %s", got)
	}
}

// A switch whose config.json flip landed, after which the app was used on
// the sign-in it put in: with switching off, recover would throw that
// sign-in away, so it changes nothing and says how to leave the app as it
// is (--leave-as-is), which keeps the app on it and ends the switch. Copies
// a switch left are listed and deleted with tidy.
func TestDesktopLoginLeaveAsIs(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	if out, code := c.run(env, "add", "work"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	work := c.registry().Get("work").AccountUUID
	const other = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	appSignIn(t, data, work, "W")
	stage := filepath.Join(data, ".baton-switch-0123456789ab")
	before := readFile(t, filepath.Join(data, "config.json"))
	appSignIn(t, data, other, "O-used")
	for p, s := range map[string]string{
		filepath.Join(stage, "journal.json"):       `{"schema":1,"from":"` + work + `","to":"` + other + `","phase":"config","cfg_before":"x","cfg_after":"y","items":[]}`,
		filepath.Join(stage, "old", "config.json"): before,
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	used := readFile(t, filepath.Join(data, "config.json"))

	out, code := c.run(env, "desktop", "login", "recover")
	if code == 0 || !strings.Contains(out, "the desktop app has been used on the sign-in a cut-off switch put in") ||
		!strings.Contains(out, "baton desktop login recover --leave-as-is") {
		t.Fatalf("recover: %d\n%s", code, out)
	}
	if out, _ := c.run(append(env, "BATON_LANG=ko"), "desktop", "login", "recover"); !strings.Contains(out, "되돌리면 그 로그인이 사라지므로 아무것도 바꾸지 않았습니다") {
		t.Fatalf("recover in Korean:\n%s", out)
	}
	if readFile(t, filepath.Join(data, "config.json")) != used || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatal("a refused recover changed something")
	}
	if out, code := c.run(append(env, "BATON_DESKTOP_RUNNING=1"), "desktop", "login", "recover", "--leave-as-is"); code == 0 || !strings.Contains(out, "still running") {
		t.Fatalf("leave as is while the app runs: %d\n%s", code, out)
	}
	// While another program holds config.json without sharing it for
	// reading, it changes nothing and says to try again in a moment, not to
	// run this very command once recover keeps failing.
	release := holdExclusive(t, filepath.Join(data, "config.json"))
	for _, lang := range []struct{ env, want string }{
		{"BATON_LANG=", "could not leave the desktop app as it is yet"},
		{"BATON_LANG=ko", "데스크톱 앱을 열지 말고 잠시 뒤 다시 시도하세요"},
	} {
		out, code := c.run(append(env, lang.env), "desktop", "login", "recover", "--leave-as-is")
		if code == 0 || !strings.Contains(out, lang.want) || strings.Contains(out, "--leave-as-is") {
			t.Fatalf("leave as is while config.json is held (%s): %d\n%s", lang.env, code, out)
		}
	}
	release()
	if readFile(t, filepath.Join(data, "config.json")) != used || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatal("a refused leave as is changed something")
	}
	out, code = c.run(env, "desktop", "login", "recover", "--leave-as-is")
	if code != 0 || !strings.Contains(out, "Left the desktop app as it is: it stays signed in to bbbbbbbb.") {
		t.Fatalf("leave as is: %d\n%s", code, out)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the stage is still there: %v", err)
	}
	if readFile(t, filepath.Join(data, "config.json")) != used || readFile(t, filepath.Join(data, "Network", "Cookies")) != "cookies-O-used" {
		t.Fatal("the app was not left as it was")
	}
	if out, code := c.run(env, "desktop", "login", "recover", "--leave-as-is"); code != 0 || !strings.Contains(out, "nothing to settle") {
		t.Fatalf("again: %d\n%s", code, out)
	}

	// A copy a switch left (it stopped before it moved anything).
	copyStage := filepath.Join(data, ".baton-switch-fedcba987654")
	if err := os.MkdirAll(filepath.Join(copyStage, "old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyStage, "old", "config.json"), []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := c.run(env, "desktop", "login"); !strings.Contains(out, "left 1 copy(ies) in the desktop app's folder") || !strings.Contains(out, "baton desktop login tidy") {
		t.Fatalf("list with a copy:\n%s", out)
	}
	if out, code := c.run(env, "desktop", "login", "tidy"); code != 0 || !strings.Contains(out, "Deleted 1 copy(ies)") {
		t.Fatalf("tidy: %d\n%s", code, out)
	}
	if _, err := os.Stat(copyStage); !os.IsNotExist(err) {
		t.Fatalf("the copy is still there: %v", err)
	}
	if out, code := c.run(env, "desktop", "login", "tidy"); code != 0 || !strings.Contains(out, "nothing to delete") {
		t.Fatalf("tidy again: %d\n%s", code, out)
	}
}

// Uninstall settles a switch that was cut off after its config.json flip,
// on which the app has been used since: the app stays on the sign-in it has
// (as uninstall says), never put back over it.
func TestUninstallLeavesTheAppAsItIs(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const other = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	appSignIn(t, data, acct, "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	appSignIn(t, data, acct, "A")
	before := readFile(t, filepath.Join(data, "config.json"))
	appSignIn(t, data, other, "O-used")
	stage := filepath.Join(data, ".baton-switch-0123456789ab")
	for p, s := range map[string]string{
		filepath.Join(stage, "journal.json"):       `{"schema":1,"from":"` + acct + `","to":"` + other + `","phase":"config","cfg_before":"x","cfg_after":"y","items":[]}`,
		filepath.Join(stage, "old", "config.json"): before,
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	used := readFile(t, filepath.Join(data, "config.json"))
	out, code := c.run(env, "uninstall", "--yes")
	if code != 0 || !strings.Contains(out, "The desktop app stays signed in to bbbbbbbb.") {
		t.Fatalf("uninstall: %d\n%s", code, out)
	}
	if readFile(t, filepath.Join(data, "config.json")) != used || readFile(t, filepath.Join(data, "Network", "Cookies")) != "cookies-O-used" {
		t.Fatalf("the app was put back over the sign-in it used:\n%s", readFile(t, filepath.Join(data, "config.json")))
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the cut-off switch is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.home, "desktop-logins")); !os.IsNotExist(err) {
		t.Fatalf("the sign-ins kept are still there: %v", err)
	}
}

// Uninstall asks whether a switch was cut off under the lock, waiting for
// it: another baton (the window's poll, say) holding the lock for a moment
// must not hide the switch, or uninstall would delete what baton keeps and
// leave the app half switched. Here the app runs, so it is refused.
func TestUninstallSeesACutOffSwitchUnderTheLock(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	appSignIn(t, data, acct, "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	appSignIn(t, data, acct, "A")
	stage := cutOffStage(t, data, acct)
	store := filepath.Join(c.home, "desktop-logins")
	unlock, err := fsx.Lock(envAt(c.home).paths.DesktopLoginsLock())
	if err != nil {
		t.Fatal(err)
	}
	let := time.AfterFunc(3*time.Second, unlock)
	defer func() {
		if let.Stop() {
			unlock()
		}
	}()
	out, code := c.run(append(env, "BATON_DESKTOP_RUNNING=1"), "uninstall", "--yes")
	if code == 0 || !strings.Contains(out, "was cut off") || !isFile(filepath.Join(store, acct, "meta.json")) || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatalf("uninstall while another baton looks: %d\n%s", code, out)
	}
}

// While another program holds the app's config.json without sharing it for
// reading, the switch that was cut off cannot be settled: uninstall stops
// before it deletes or moves anything (the conversations stay in baton's
// profiles: moved first, baton would be left half uninstalled) and says to
// try again in a moment. Once it is let go, uninstall settles it and goes
// on.
func TestUninstallWaitsForTheAppsConfigToBeLetGo(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	appSignIn(t, data, acct, "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	appSignIn(t, data, acct, "A")
	stage := cutOffStage(t, data, acct)
	store := filepath.Join(c.home, "desktop-logins")
	conv, moved := oneConversation(t, c, env)
	release := holdExclusive(t, filepath.Join(data, "config.json"))
	out, code := c.run(env, "uninstall", "--yes")
	release()
	if code == 0 || !strings.Contains(out, "baton was not removed") || !strings.Contains(out, "try again in a moment") ||
		!isFile(filepath.Join(store, acct, "meta.json")) || !isFile(filepath.Join(stage, "journal.json")) {
		t.Fatalf("uninstall while config.json is held: %d\n%s", code, out)
	}
	if strings.Contains(out, "moved 1 conversation") || !isFile(conv) || isFile(moved) {
		t.Fatalf("uninstall while config.json is held moved the conversation: %d\n%s", code, out)
	}
	if out, code := c.run(env, "uninstall", "--yes"); code != 0 || !strings.Contains(out, "moved 1 conversation") || !isFile(moved) {
		t.Fatalf("uninstall once it is let go: %d\n%s", code, out)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("the cut-off switch is still there: %v", err)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("the sign-ins kept are still there: %v", err)
	}
	if readFile(t, filepath.Join(data, "Network", "Cookies")) != "cookies-A" {
		t.Fatal("the app's sign-in changed")
	}
}

// oneConversation adds baton's account a with one conversation in its
// profile: where it is, and where uninstalling moves it (the user's own
// Claude Code).
func oneConversation(t *testing.T, c *cli, env []string) (conv, moved string) {
	t.Helper()
	if out, code := c.run(env, "add", "a"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	const sid = "11111111-2222-4333-8444-0000000000c1"
	conv = filepath.Join(c.registry().Get("a").ProfileDir, "projects", "C--proj", sid+".jsonl")
	moved = filepath.Join(envValue(env, "CLAUDE_CONFIG_DIR"), "projects", "C--proj", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(conv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conv, []byte(`{"type":"user","cwd":"C:\proj"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return conv, moved
}

// The desktop app was started after uninstall looked (here: its lockfile is
// held), so the switch that was cut off cannot be settled: uninstall moves
// and deletes nothing, and says once what to do (quit the app completely,
// then try again), in either language.
func TestUninstallSaysOnceWhatToDo(t *testing.T) {
	c := newCLI(t)
	env, data := loginEnv(t)
	setDesktopLogins(t, c.home, true, config.DesktopLoginsDisclosure)
	const acct = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	appSignIn(t, data, acct, "A")
	if out, code := c.run(env, "desktop", "login", "add"); code != 0 {
		t.Fatalf("add: %d\n%s", code, out)
	}
	appSignIn(t, data, acct, "A")
	stage := cutOffStage(t, data, acct)
	store := filepath.Join(c.home, "desktop-logins")
	conv, moved := oneConversation(t, c, env)
	lock := filepath.Join(data, "lockfile")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdExclusive(t, lock)
	defer release()
	for _, lang := range []struct{ env, running, again string }{
		{"BATON_LANG=", "the Claude desktop app is still running", "try again"},
		{"BATON_LANG=ko", "Claude 데스크톱 앱이 아직 실행 중입니다", "다시 시도하세요"},
	} {
		out, code := c.run(append(env, lang.env), "uninstall", "--yes")
		if code == 0 || !strings.Contains(out, lang.running) || strings.Count(strings.ToLower(out), lang.again) != 1 {
			t.Errorf("uninstall while the app holds its lockfile (%s): %d\n%s", lang.env, code, out)
		}
		if !isFile(conv) || isFile(moved) || !isFile(filepath.Join(store, acct, "meta.json")) || !isFile(filepath.Join(stage, "journal.json")) {
			t.Fatalf("uninstall while the app holds its lockfile (%s) moved or deleted something: %d\n%s", lang.env, code, out)
		}
	}
}

// A sign-in baton keeps that it cannot read is named, with the command that
// forgets it: trying again does not help when it never can be read. recover
// adds how to leave the app as it is instead. A label may hold almost any
// character, and the command is pasted into cmd or PowerShell: bare, R&D
// runs "forget R" and then a command D in cmd, and PowerShell refuses the
// line, so such a name is quoted. A name that no quoting keeps whole in both
// (a " or $, ` or %, a \ before the closing quote, a leading - read as an
// option) gets no command: it is forgotten by its id, which the list shows.
func TestKeptUnreadableIsNamed(t *testing.T) {
	t.Setenv("BATON_LANG", "")
	const byID = "find its id, shown in [ ] on its line in baton desktop login list, then run: baton desktop login forget <id>"
	for _, c := range []struct{ name, forget string }{
		{"home", "baton desktop login forget home"},
		{"a.b+c_d-e@example.com", "baton desktop login forget a.b+c_d-e@example.com"},
		{"회사", "baton desktop login forget 회사"},
		{"My Work", `baton desktop login forget "My Work"`},
		{"R&D", `baton desktop login forget "R&D"`},
		{"a>b|c^d", `baton desktop login forget "a>b|c^d"`},
		{"(it's)", `baton desktop login forget "(it's)"`},
		{"a;b#c", `baton desktop login forget "a;b#c"`},
		{"@work", `baton desktop login forget "@work"`},     // bare, PowerShell splats it
		{`C:\Work`, `baton desktop login forget "C:\Work"`}, // a \ not before the closing quote is itself
		{`say"hi"`, byID}, // " ends the quotes in both
		{"say“hi”", byID}, // PowerShell reads “ and ” as "
		{"$env:x", byID},  // PowerShell expands it inside "..."
		{"a`b", byID},     // PowerShell's escape
		{"50%x%", byID},   // cmd expands it inside "..."
		{`a b\`, byID},    // "a b\" escapes the closing quote
		{"-x", byID},      // an option to baton however it is quoted
	} {
		err := &desktop.Error{Code: desklogin.CodeUnsavedCurrent, Detail: "kept_unreadable:" + c.name}
		if got := loginErr(err).Error(); !strings.Contains(got, "baton could not read the sign-in it keeps for "+c.name+",") || !strings.Contains(got, c.forget) {
			t.Errorf("%s:\n%s", c.name, got)
		}
		if got := recoverErr(err).Error(); !strings.Contains(got, c.forget) || !strings.Contains(got, "baton desktop login recover --leave-as-is") {
			t.Errorf("recover, %s:\n%s", c.name, got)
		}
		if got := loginErr(err).Error(); c.forget == byID && strings.Count(got, "baton desktop login forget") != 1 {
			t.Errorf("%s: a command with the name as it is:\n%s", c.name, got)
		}
	}
	t.Setenv("BATON_LANG", "ko")
	got := loginErr(&desktop.Error{Code: desklogin.CodeUnsavedCurrent, Detail: "kept_unreadable:home"}).Error()
	if !strings.Contains(got, "baton이 보관한 home의 로그인을 읽지 못해") || !strings.Contains(got, "보관한 그 로그인을 지우세요: baton desktop login forget home") {
		t.Errorf("in Korean:\n%s", got)
	}
	got = loginErr(&desktop.Error{Code: desklogin.CodeUnsavedCurrent, Detail: "kept_unreadable:R&D"}).Error()
	if !strings.Contains(got, "baton이 보관한 R&D의 로그인을 읽지 못해") || !strings.Contains(got, `보관한 그 로그인을 지우세요: baton desktop login forget "R&D"`) {
		t.Errorf("in Korean, R&D:\n%s", got)
	}
	got = loginErr(&desktop.Error{Code: desklogin.CodeUnsavedCurrent, Detail: "kept_unreadable:$env:x"}).Error()
	if !strings.Contains(got, "baton이 보관한 $env:x의 로그인을 읽지 못해") ||
		!strings.Contains(got, "보관한 그 로그인을 지우세요. baton desktop login list에서 그 로그인 옆 [ ] 안의 ID를 확인한 뒤 baton desktop login forget <ID>를 실행하세요") {
		t.Errorf("in Korean, $env:x:\n%s", got)
	}
}

// recover's hint to leave the app as it is reads naturally in Korean: the
// app stays on the account it is signed in to now.
func TestRecoverHintInKorean(t *testing.T) {
	t.Setenv("BATON_LANG", "ko")
	for _, err := range []error{
		&desktop.Error{Code: desklogin.CodeUnsavedCurrent, Detail: "no_account"},
		&desktop.Error{Code: desklogin.CodeDamaged, Detail: "tokens_of_another"},
	} {
		const want = "또는 데스크톱 앱을 지금 그대로 둘 수 있습니다(앱은 지금 로그인된 계정에 남음): baton desktop login recover --leave-as-is"
		if got := recoverErr(err).Error(); !strings.Contains(got, want) {
			t.Errorf("%v:\n%s", err, got)
		}
	}
}

// holdExclusive opens p as another program may: for reading, sharing it with
// no one, so baton can neither read nor replace it until release.
func holdExclusive(t *testing.T, p string) (release func()) {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(u, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(release)
	return release
}

// The top-level usage names every subcommand of baton desktop login, and
// the options that change what one does (--open and --yes only add to it).
func TestUsageNamesEveryDesktopLoginCommand(t *testing.T) {
	top := usageText[strings.Index(usageText, "baton desktop login"):]
	top = top[:strings.Index(top, "\n  baton ")]
	cols := regexp.MustCompile(`\s{2,}`)
	n := 0
	for _, line := range strings.Split(loginUsage, "\n") {
		f := strings.Fields(cols.Split(strings.TrimSpace(line), 2)[0])
		if len(f) < 4 || strings.Join(f[:3], " ") != "baton desktop login" {
			continue
		}
		for i, w := range f[3:] {
			w = strings.Trim(w, "[]")
			if i == 0 && w == "list" || w == "--open" || w == "--yes" || strings.HasPrefix(w, "<") {
				continue
			}
			n++
			if !strings.Contains(top, w) {
				t.Errorf("baton --help leaves out %q of: %s", w, strings.TrimSpace(line))
			}
		}
	}
	if n < 8 {
		t.Fatalf("read only %d words of the desktop login usage", n)
	}
}
