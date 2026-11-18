package desklogin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/config"
	"baton/internal/desktop"
)

// Every folder in these tests is under t.TempDir(): a fake desktop app data
// folder shaped like the real one (as the prototype found it), and baton's
// folder. The write guard (desktop.Guard) panics on anything else.

const (
	acctA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	acctB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	acctC = "aaaaaaaa-cccc-4ccc-8ccc-cccccccccccc" // starts as A does: an 8-digit prefix fits both
	orgA  = "a0000000-0000-4000-8000-00000000000a"
	orgB  = "b0000000-0000-4000-8000-00000000000b"
)

type rig struct {
	t     *testing.T
	root  string
	data  string // the desktop app's data folder
	paths app.Paths
	l     *Logins
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return newRigAt(t, filepath.Join("Claude-Data", "claude_test"))
}

// newRigAt is newRig with the app's data folder at rel in the test's
// folder. The older installer's folder (%APPDATA%\Claude) is the test's
// roaming\Claude.
func newRigAt(t *testing.T, rel string) *rig {
	t.Helper()
	root := t.TempDir()
	r := &rig{t: t, root: root, data: filepath.Join(root, rel),
		paths: app.Paths{Root: filepath.Join(root, "baton")}}
	t.Setenv("BATON_DESKTOP_DATA", r.data)
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	for _, k := range []string{"CLAUDECODE", "OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		t.Setenv(k, "")
	}
	must(t, os.MkdirAll(r.paths.Root, 0o700))
	r.l = New(r.paths, testAccounts())
	r.l.UserHome = filepath.Join(root, "user", ".claude")
	r.l.Elevated = func() bool { return false }
	r.l.wait, r.l.appLockWait = 300*time.Millisecond, 200*time.Millisecond
	r.l.Now = func() time.Time { return time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC) }
	r.setOn(true)
	for p, c := range untouched {
		r.write(p, c)
	}
	r.signIn(acctA, "A")
	return r
}

// testAccounts are baton's accounts in these tests: A and B (C is none of
// baton's).
func testAccounts() accounts.Registry {
	return accounts.Registry{Accounts: []accounts.Account{
		{ID: "work", Priority: 20, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "a@example.com",
			AccountUUID: acctA, OrgID: orgA, OrgName: "A org"},
		{ID: "home", Priority: 10, Enabled: true, LoggedIn: true, AuthMethod: "claude.ai", Email: "b@example.com",
			AccountUUID: acctB, OrgID: orgB},
	}}
}

// untouched are files of the app's data folder no switch moves.
var untouched = map[string]string{
	filepath.Join("claude-code-sessions", acctA, orgA, "local_x.json"): `{"sessionId":"local_x"}`,
	"ant-did":                  "device",
	"ant-device-registry.json": "{}",
	filepath.Join("Network", "TransportSecurity"): "hsts",
	"Local State":       `{"os_crypt":{"encrypted_key":"k"}}`,
	"window-state.json": "{}",
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (r *rig) setOn(on bool) {
	r.t.Helper()
	_, err := config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.DesktopLogins, c.DesktopLoginsAcknowledged = on, 0
		if on {
			c.DesktopLoginsAcknowledged = config.DesktopLoginsDisclosure
		}
		return nil
	})
	must(r.t, err)
}

func (r *rig) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.data, rel)
	must(r.t, os.MkdirAll(filepath.Dir(p), 0o700))
	must(r.t, os.WriteFile(p, []byte(content), 0o600))
}

func (r *rig) read(rel string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.data, rel))
	must(r.t, err)
	return string(b)
}

// loginOf are the six values the app writes for account acct; tag makes
// them this sign-in's own.
func loginOf(acct, tag string) []kv {
	return []kv{
		{"oauth:tokenCache", []byte(`"sealed-` + tag + `-1"`)},
		{"oauth:tokenCacheV2", []byte(`"sealed2-` + tag + `-1"`)},
		{"lastKnownAccountUuid", []byte(`"` + acct + `"`)},
		{"bootSideSessions", []byte("{\n    \"items\": [1, 2, {\"t\": \"" + tag + " <title> & co\"}]\n  }")},
		{"bootSidebarPlaceholder", []byte(`"` + tag + ` placeholder"`)},
		{"sshTransportFlagLastAnswer", []byte(`false`)},
	}
}

// appCfg is the app's config.json as it writes it (indented by two, no
// line end at the end), with login among its other settings.
func appCfg(login []kv, nl string) []byte {
	ms := []string{
		`"updaterLastSeenVersion": "5.11492.0.0"`,
		`"locale": "ko-KR"`,
		`"dxt:allowlistEnabled:` + orgA + `": true`,
		`"html": "<b>&amp;</b> \u2028 raw:` + "\u2028" + ` \u00e9"`,
		`"big": 123456789012345678901234567890`,
		`"korean": "한국어 ✓"`,
	}
	for _, k := range login {
		ms = append(ms, `"`+k.key+`": `+string(k.raw))
	}
	ms = append(ms, `"windowSizeWasSignedIn": {`+nl+`    "w": 1200,`+nl+`    "h": 800`+nl+`  }`)
	return []byte("{" + nl + "  " + strings.Join(ms, ","+nl+"  ") + nl + "}")
}

// itemFiles are the files of a login set tagged tag.
func itemFiles(tag string) map[string]string {
	return map[string]string{
		filepath.Join("Local Storage", "leveldb", "000003.log"):                         "ls-" + tag,
		filepath.Join("Local Storage", "leveldb", "CURRENT"):                            "MANIFEST-000001\n",
		filepath.Join("Session Storage", "000003.log"):                                  "ss-" + tag,
		filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log"): "idb-" + tag,
		filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.blob", "1", "00", "01"): "blob-" + tag,
		filepath.Join("WebStorage", "2", "IndexedDB", "x"):                              "ws-" + tag,
		filepath.Join("WebStorage", "QuotaManager"):                                     "qm-" + tag,
		filepath.Join("WebStorage", "QuotaManager-journal"):                             "",
		filepath.Join("Network", "Cookies"):                                             "cookies-" + tag,
		filepath.Join("Network", "Cookies-journal"):                                     "",
	}
}

// signIn makes the app look signed in to acct, as the app would leave it.
func (r *rig) signIn(acct, tag string) {
	r.t.Helper()
	r.clearItems()
	for p, c := range itemFiles(tag) {
		r.write(p, c)
	}
	r.writeCfg(appCfg(loginOf(acct, tag), "\n"))
}

func (r *rig) clearItems() {
	for _, it := range items {
		must(r.t, os.RemoveAll(filepath.Join(r.data, it.name)))
	}
}

func (r *rig) writeCfg(b []byte) {
	r.t.Helper()
	must(r.t, os.MkdirAll(r.data, 0o700))
	must(r.t, os.WriteFile(filepath.Join(r.data, "config.json"), b, 0o600))
}

func (r *rig) cfg() *object {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.data, "config.json"))
	must(r.t, err)
	o, why := parseObject(b)
	if o == nil {
		r.t.Fatalf("config.json: %s", why)
	}
	return o
}

// itemsIn are the login set's files under dir, by path, with their content.
func itemsIn(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, it := range items {
		root := filepath.Join(dir, it.name)
		if _, err := os.Lstat(root); err != nil {
			continue
		}
		must(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			out[rel] = string(b)
			return nil
		}))
	}
	return out
}

// tree is every file and folder under dir: a file by its content's hash.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if _, err := os.Stat(dir); err != nil {
		return out
	}
	must(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			out[rel] = "dir"
			return nil
		}
		if !d.Type().IsRegular() {
			out[rel] = "link"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(h[:])
		return nil
	}))
	return out
}

func sameTree(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	if maps.Equal(want, got) {
		return
	}
	var diff []string
	for k, v := range want {
		if got[k] != v {
			diff = append(diff, "changed or gone: "+k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			diff = append(diff, "new: "+k)
		}
	}
	slices.Sort(diff)
	t.Fatalf("%s:\n%s", what, strings.Join(diff, "\n"))
}

func wantCode(t *testing.T, what string, err error, code string) {
	t.Helper()
	if got := Code(err); got != code {
		t.Fatalf("%s: got %v, want %s", what, err, code)
	}
}

// keptLogin is what keys.json holds for acct.
func (r *rig) keptLogin(acct string) []kv {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.l.Store, acct, "keys.json"))
	must(r.t, err)
	k, ok := readKeys(b)
	if !ok {
		r.t.Fatalf("keys.json of %s is not whole", acct)
	}
	return k
}

func sameLogin(t *testing.T, what string, want, got []kv) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: %d values, want %d", what, len(got), len(want))
	}
	for i := range want {
		if want[i].key != got[i].key || string(want[i].raw) != string(got[i].raw) {
			t.Fatalf("%s: %s differs", what, want[i].key)
		}
	}
}

// noStage: no switch's stage is left in the app's folder.
func (r *rig) noStage() {
	r.t.Helper()
	if s := stages(r.data, false); len(s) > 0 {
		r.t.Fatalf("a stage is left: %v", s)
	}
}

// stopAt makes the next switch stop at step: with errCrash as if baton were
// killed there, else as a failure there.
func (r *rig) stopAt(step string, err error) {
	r.l.failAt = func(s string) error {
		if s == step {
			return err
		}
		return nil
	}
}

var errTest = errors.New("failed here (test)")

// keptAll: A kept (by a Blank), then the app signed in to B and switched
// back to A, so B is kept too. The app is on A.
func (r *rig) keptAll() {
	r.t.Helper()
	_, err := r.l.Blank()
	must(r.t, err)
	r.signIn(acctB, "B")
	_, err = r.l.Use(acctA)
	must(r.t, err)
	if desktop.Active(r.data) != acctA {
		r.t.Fatal("setup: not on A")
	}
}
