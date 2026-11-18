package desklogin

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"baton/internal/config"
	"baton/internal/desktop"
	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// Blank keeps A and empties the app's sign-in; after a sign-in to B there,
// Use(A) puts A back exactly as it was kept, and keeps B.
func TestBlankThenSignInThenUse(t *testing.T) {
	r := newRig(t)
	before := tree(t, r.data)
	itemsA, loginA := itemsIn(t, r.data), r.cfg().login()
	restA := r.cfg().rest()

	res, err := r.l.Blank()
	must(t, err)
	if res.From != acctA || res.To != "" || !res.Kept || res.Recovered {
		t.Fatalf("blank: %+v", res)
	}
	if got := itemsIn(t, r.data); len(got) != 0 {
		t.Fatalf("blank left items: %v", got)
	}
	// Every other setting keeps its bytes and order; the file is exactly
	// what the app would write without the six.
	if got, want := r.read("config.json"), string(appCfg(nil, "\n")); got != want {
		t.Fatalf("config after blank:\n%s\nwant:\n%s", got, want)
	}
	after := tree(t, r.data)
	for p := range untouched {
		if after[p] != before[p] {
			t.Fatalf("%s changed", p)
		}
	}
	if desktop.Active(r.data) != "" {
		t.Fatal("still on an account")
	}
	r.noStage()
	if why := checkFolder(filepath.Join(r.l.Store, acctA), acctA); why != "" {
		t.Fatalf("A kept: %s", why)
	}
	if got := itemsIn(t, filepath.Join(r.l.Store, acctA)); !maps.Equal(got, itemsA) {
		t.Fatalf("A's kept items differ")
	}
	sameLogin(t, "A kept", loginA, r.keptLogin(acctA))
	if !exists(filepath.Join(r.l.Store, "README.txt")) {
		t.Fatal("no README in the store")
	}

	// The user signs in to B in the app.
	r.signIn(acctB, "B")
	itemsB, loginB := itemsIn(t, r.data), r.cfg().login()
	res, err = r.l.Use(acctA)
	must(t, err)
	if res.From != acctB || res.To != acctA || !res.Kept {
		t.Fatalf("use A: %+v", res)
	}
	if got := itemsIn(t, r.data); !maps.Equal(got, itemsA) {
		t.Fatalf("the app's items are not A's")
	}
	o := r.cfg()
	sameLogin(t, "the app's six", loginA, o.login())
	if rest := o.rest(); len(rest) != len(restA) {
		t.Fatal("other settings changed")
	} else {
		for i := range rest {
			if string(rest[i]) != string(restA[i]) {
				t.Fatalf("setting %d changed", i)
			}
		}
	}
	if desktop.Active(r.data) != acctA {
		t.Fatal("not on A")
	}
	if got := itemsIn(t, filepath.Join(r.l.Store, acctB)); !maps.Equal(got, itemsB) {
		t.Fatal("B's kept items differ")
	}
	sameLogin(t, "B kept", loginB, r.keptLogin(acctB))
	r.noStage()
	m, err := readMeta(filepath.Join(r.l.Store, acctA))
	must(t, err)
	if m.Used.IsZero() || m.Email != "a@example.com" || m.Org != orgA || m.OrgName != "A org" || m.FirstKept.IsZero() {
		t.Fatalf("A's meta: %+v", m)
	}
	for p, c := range untouched {
		if r.read(p) != c {
			t.Fatalf("%s changed", p)
		}
	}

	wantCode(t, "use of one not kept", useErr(r.l.Use(acctC)), CodeNotKept)
	wantCode(t, "use of the one it is on", useErr(r.l.Use(acctA)), CodeAlready)
	wantCode(t, "use of no account", useErr(r.l.Use("..\\x")), CodeNotKept)
}

func useErr(_ Switched, err error) error { return err }

// The app renews its tokens as it runs: every switch keeps what it has now,
// so the renewed ones come back, never the ones kept before.
func TestRenewedTokensKeptBeforeEverySwitch(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.signIn(acctA, "A2") // the app renewed A's sign-in
	itemsA2, loginA2 := itemsIn(t, r.data), r.cfg().login()
	if _, err := r.l.Use(acctB); err != nil {
		t.Fatal(err)
	}
	sameLogin(t, "A kept", loginA2, r.keptLogin(acctA))
	if _, err := r.l.Use(acctA); err != nil {
		t.Fatal(err)
	}
	sameLogin(t, "A back", loginA2, r.cfg().login())
	if !maps.Equal(itemsIn(t, r.data), itemsA2) {
		t.Fatal("A's renewed items did not come back")
	}
}

// A switch needs the app fully quit: no process of it, and its lockfile
// free. Holding that file is enough to refuse, whatever
// BATON_DESKTOP_RUNNING says.
func TestRefusesWhileRunning(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	r.signIn(acctB, "B")
	before, store := tree(t, r.data), tree(t, r.l.Store)

	t.Setenv("BATON_DESKTOP_RUNNING", "1")
	wantCode(t, "use while running", useErr(r.l.Use(acctA)), CodeAppRunning)
	wantCode(t, "blank while running", useErr(r.l.Blank()), CodeAppRunning)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))

	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	p, _ := windows.UTF16PtrFromString(filepath.Join(r.data, "lockfile"))
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	must(t, err)
	before = tree(t, r.data)
	wantCode(t, "use while the lockfile is held", useErr(r.l.Use(acctA)), CodeAppRunning)
	wantCode(t, "blank while the lockfile is held", useErr(r.l.Blank()), CodeAppRunning)
	wantCode(t, "recover while the lockfile is held", recErr(r.l.Recover()), CodeAppRunning)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
	windows.CloseHandle(h)
	must(t, os.Remove(filepath.Join(r.data, "lockfile")))

	// Free again: it switches, and its own hold on the lockfile goes with it.
	if _, err := r.l.Use(acctA); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(r.data, "lockfile")) {
		t.Fatal("the lockfile baton held is left behind")
	}
}

func recErr(_ bool, err error) error { return err }

// Anything that is not how baton knows the app keeps its sign-in changes
// nothing.
func TestUnknownFormat(t *testing.T) {
	cases := map[string]func(r *rig){
		"a third token key": func(r *rig) {
			r.writeCfg(appCfg(append(loginOf(acctB, "B"), kv{"oauth:tokenCacheV3", []byte(`"x"`)}), "\n"))
		},
		"an account that is no id": func(r *rig) {
			l := loginOf(acctB, "B")
			l[2].raw = []byte(`"not-an-id"`)
			r.writeCfg(appCfg(l, "\n"))
		},
		"a token that is no string": func(r *rig) {
			l := loginOf(acctB, "B")
			l[1].raw = []byte(`{"v": 1}`)
			r.writeCfg(appCfg(l, "\n"))
		},
		"a key twice": func(r *rig) {
			r.writeCfg(appCfg(append(loginOf(acctB, "B"), kv{"locale", []byte(`"en"`)}), "\n"))
		},
		"no object":      func(r *rig) { r.writeCfg([]byte(`["x"]`)) },
		"no JSON":        func(r *rig) { r.writeCfg([]byte(`{"locale": `)) },
		"no config":      func(r *rig) { must(r.t, os.Remove(filepath.Join(r.data, "config.json"))) },
		"cookies in WAL": func(r *rig) { r.write(filepath.Join("Network", "Cookies-wal"), "wal") },
		"cookies shm":    func(r *rig) { r.write(filepath.Join("Network", "Cookies-shm"), "shm") },
		"a partition":    func(r *rig) { must(r.t, os.MkdirAll(filepath.Join(r.data, "Partitions", "x"), 0o700)) },
		"a junction in IndexedDB": func(r *rig) {
			other := filepath.Join(r.root, "elsewhere")
			must(r.t, os.MkdirAll(other, 0o700))
			must(r.t, fsx.CreateJunction(filepath.Join(r.data, "IndexedDB", "link"), other))
		},
		"a junction for an item": func(r *rig) {
			other := filepath.Join(r.root, "elsewhere")
			must(r.t, os.MkdirAll(other, 0o700))
			must(r.t, os.RemoveAll(filepath.Join(r.data, "Session Storage")))
			must(r.t, fsx.CreateJunction(filepath.Join(r.data, "Session Storage"), other))
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			_, err := r.l.Blank()
			must(t, err)
			r.signIn(acctB, "B")
			spoil(r)
			before, store := tree(t, r.data), tree(t, r.l.Store)
			wantCode(t, "use", useErr(r.l.Use(acctA)), CodeUnknownFormat)
			wantCode(t, "blank", useErr(r.l.Blank()), CodeUnknownFormat)
			sameTree(t, "the app's folder", before, tree(t, r.data))
			sameTree(t, "the store", store, tree(t, r.l.Store))
		})
	}
}

// The app signed in, but quit before it said to which account: that
// sign-in has nowhere to be kept, and is never thrown away.
func TestSignedInWithoutAccountIsKeptInPlace(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	l := loginOf(acctB, "B")
	r.signIn(acctB, "B")
	r.writeCfg(appCfg(append(l[:2:2], l[3:]...), "\n")) // no lastKnownAccountUuid
	before, store := tree(t, r.data), tree(t, r.l.Store)
	wantCode(t, "use", useErr(r.l.Use(acctA)), CodeUnsavedCurrent)
	wantCode(t, "blank", useErr(r.l.Blank()), CodeUnsavedCurrent)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
	// One token is enough to refuse.
	r.writeCfg(appCfg([]kv{l[1]}, "\n"))
	wantCode(t, "use with one token", useErr(r.l.Use(acctA)), CodeUnsavedCurrent)

	// Nothing at all: signed out already.
	r.writeCfg(appCfg(nil, "\n"))
	r.clearItems()
	wantCode(t, "blank when signed out", useErr(r.l.Blank()), CodeAlready)
	if _, err := r.l.Use(acctA); err != nil {
		t.Fatalf("use from signed out: %v", err)
	}
	if desktop.Active(r.data) != acctA {
		t.Fatal("not on A")
	}
	if v, _ := r.l.View(); len(v.Unnamed) != 0 {
		t.Fatalf("an empty app left something: %+v", v.Unnamed)
	}
}

// Opened and quit without a sign-in after a Blank, the app makes cookies and
// site storage of its own, with no account and no token. That never stops
// the way back to a kept sign-in: it is set aside in the store as
// unnamed-<when>, listed as such, and the switch goes on. It goes only when
// the user forgets it.
func TestSignedOutLeftoversSetAside(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	r.write(filepath.Join("Network", "Cookies"), "cookies-nobody")
	r.write(filepath.Join("Local Storage", "leveldb", "000003.log"), "ls-nobody")
	nobody := []kv{{"oauth:tokenCache", []byte(`""`)}, {"bootSidebarPlaceholder", []byte(`"signed out"`)}}
	r.writeCfg(appCfg(nobody, "\n"))
	leftItems := itemsIn(t, r.data)

	res, err := r.l.Use(acctA)
	must(t, err)
	want := stashPrefix + r.l.Now().Local().Format("20060102-150405")
	if res.Stashed != want || res.Kept || res.From != "" || res.To != acctA {
		t.Fatalf("use: %+v", res)
	}
	if desktop.Active(r.data) != acctA || !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
		t.Fatal("the app is not on A")
	}
	sameLogin(t, "the app's six", loginOf(acctA, "A"), r.cfg().login())
	r.noStage()
	dir := filepath.Join(r.l.Store, want)
	if !maps.Equal(itemsIn(t, dir), leftItems) {
		t.Fatalf("set aside: %v", itemsIn(t, dir))
	}
	kb, err := os.ReadFile(filepath.Join(dir, "keys.json"))
	must(t, err)
	k, ok := readKeys(kb)
	if !ok {
		t.Fatal("keys.json of the leftovers is not whole")
	}
	sameLogin(t, "the leftovers' values", nobody, k)
	v, err := r.l.View()
	must(t, err)
	if len(v.Unnamed) != 1 || v.Unnamed[0].Name != want || v.Unnamed[0].Size == 0 || !v.Unnamed[0].KeptAt.Equal(r.l.Now()) || v.Kept != 1 {
		t.Fatalf("view: %+v", v)
	}
	for _, g := range v.Logins {
		if strings.HasPrefix(g.Label, stashPrefix) || !desktop.ValidID(g.Account) {
			t.Fatalf("listed as a sign-in: %+v", g)
		}
	}
	// It is no account to switch to, nor to name, but it can be forgotten.
	if got, err := r.l.Resolve(strings.ToUpper(want)); err != nil || got != want {
		t.Fatalf("resolve: %s %v", got, err)
	}
	wantCode(t, "use of it", useErr(r.l.Use(want)), CodeNotKept)
	wantCode(t, "rename of it", r.l.Rename(want, "x"), CodeNotKept)

	// Blank does the same, and a second one in the same second gets a name
	// of its own.
	r.clearItems()
	r.write(filepath.Join("Network", "Cookies"), "cookies-nobody-2")
	r.writeCfg(appCfg(nil, "\n"))
	res, err = r.l.Blank()
	must(t, err)
	if res.Stashed != want+"-2" || len(itemsIn(t, r.data)) != 0 {
		t.Fatalf("blank: %+v", res)
	}
	if c, _ := os.ReadFile(filepath.Join(r.l.Store, want+"-2", "Network", "Cookies")); string(c) != "cookies-nobody-2" {
		t.Fatalf("the second leftovers: %q", c)
	}

	// A switch that is undone takes nothing aside: the app has it all again.
	r.write(filepath.Join("Network", "Cookies"), "cookies-nobody-3")
	before, store := tree(t, r.data), tree(t, r.l.Store)
	r.stopAt("config", errTest)
	wantCode(t, "a failed use", useErr(r.l.Use(acctA)), CodeSwitchFailed)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
	// Stopped while it is set aside: the next switch sweeps the half copy.
	r.stopAt("stash", errCrash)
	if _, err := r.l.Use(acctA); err != errCrash {
		t.Fatalf("crash: %v", err)
	}
	sameTree(t, "the app's folder after a stop", before, tree(t, r.data))
	r.l.failAt = nil
	res, err = r.l.Use(acctA)
	must(t, err)
	if res.Stashed != want+"-3" {
		t.Fatalf("after the stop: %+v", res)
	}
	ents, _ := os.ReadDir(r.l.Store)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("left in the store: %s", e.Name())
		}
	}

	must(t, r.l.Drop(want))
	if exists(dir) || !exists(filepath.Join(r.l.Store, want+"-2")) {
		t.Fatal("drop of the leftovers")
	}
	wantCode(t, "drop again", r.l.Drop(want), CodeNotKept)
	wantCode(t, "resolve once gone", errOf(r.l.Resolve(want)), CodeNotKept)
	if n, err := r.l.DropAll(); err != nil || n != 1 || exists(r.l.Store) {
		t.Fatalf("drop all: %d %v", n, err)
	}
}

func errOf(_ string, err error) error { return err }

// The account the app names is the only sign of whose tokens these are: if
// they are another kept account's as they are, the name is stale, and
// nothing is kept under it.
func TestTokensOfAnotherAccountRefused(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	l := loginOf(acctB, "B")
	l[1] = loginOf(acctA, "A")[1] // A's sealed tokens, named B
	r.signIn(acctB, "B")
	r.writeCfg(appCfg(l, "\n"))
	before, store := tree(t, r.data), tree(t, r.l.Store)
	wantCode(t, "use", useErr(r.l.Use(acctA)), CodeDamaged)
	wantCode(t, "blank", useErr(r.l.Blank()), CodeDamaged)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
}

// "Already" needs the app signed in to it: a kept sign-in can be put back
// into an app that names the account but has no token.
func TestAlreadyNeedsAToken(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	wantCode(t, "use of the one it is on", useErr(r.l.Use(acctA)), CodeAlready)
	l := loginOf(acctA, "A")
	r.clearItems()
	r.writeCfg(appCfg(l[2:], "\n")) // names A, no token, no items
	res, err := r.l.Use(acctA)
	if err != nil || res.Kept {
		t.Fatalf("use into a signed-out app: %+v %v", res, err)
	}
	sameLogin(t, "A back", loginOf(acctA, "A"), r.cfg().login())
	if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
		t.Fatal("A's items did not come back")
	}
}

// A kept sign-in that is not whole is never put into the app.
func TestDamagedKeptRefused(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	must(t, os.WriteFile(filepath.Join(r.l.Store, acctB, "Network", "Cookies"), []byte("short"), 0o600))
	before := tree(t, r.data)
	wantCode(t, "use of a damaged one", useErr(r.l.Use(acctB)), CodeDamaged)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	must(t, os.Remove(filepath.Join(r.l.Store, acctB, "meta.json")))
	wantCode(t, "use without meta", useErr(r.l.Use(acctB)), CodeDamaged)
	v, err := r.l.View()
	must(t, err)
	for _, g := range v.Logins {
		if g.Account == acctB && !g.Damaged {
			t.Fatal("the view does not say B is damaged")
		}
	}
}

// Off, or on with no notice as it reads now accepted, nothing switches;
// deleting still works.
func TestFeatureOff(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.setOn(false)
	before := tree(t, r.data)
	wantCode(t, "use", useErr(r.l.Use(acctB)), CodeOff)
	wantCode(t, "blank", useErr(r.l.Blank()), CodeOff)
	wantCode(t, "rename", r.l.Rename(acctB, "x"), CodeOff)
	sameTree(t, "the app's folder", before, tree(t, r.data))
	r.setOn(true)
	r.setAck(0)
	wantCode(t, "use without the notice", useErr(r.l.Use(acctB)), CodeOff)
	if n, err := r.l.DropAll(); err != nil || n != 2 || exists(r.l.Store) {
		t.Fatalf("drop all while off: %d %v", n, err)
	}
}

func (r *rig) setAck(n int) {
	r.t.Helper()
	_, err := config.Update(r.paths.Config(), r.paths.ConfigLock(), func(c *config.Config) error {
		c.DesktopLoginsAcknowledged = n
		return nil
	})
	must(r.t, err)
}

// Inside a Claude Code, as administrator, with a store that syncs off this
// PC, or not the layout baton knows: nothing switches.
func TestRefusals(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	r.signIn(acctB, "B")
	before := tree(t, r.data)

	t.Setenv("CLAUDECODE", "1")
	wantCode(t, "use inside Claude Code", useErr(r.l.Use(acctA)), CodeInsideClaude)
	wantCode(t, "blank inside Claude Code", useErr(r.l.Blank()), CodeInsideClaude)
	wantCode(t, "recover inside Claude Code", recErr(r.l.Recover()), CodeInsideClaude)
	t.Setenv("CLAUDECODE", "")

	r.l.Elevated = func() bool { return true }
	wantCode(t, "use as administrator", useErr(r.l.Use(acctA)), CodeElevated)
	r.l.Elevated = func() bool { return false }

	t.Setenv("OneDrive", r.paths.Root)
	wantCode(t, "use with a synced store", useErr(r.l.Use(acctA)), CodeSyncedStore)
	wantCode(t, "turn on with a synced store", TurnOn(r.paths, true), CodeSyncedStore)
	t.Setenv("OneDrive", "")

	t.Setenv("BATON_DESKTOP_DATA", "")
	wantCode(t, "use without the app's folder", useErr(r.l.Use(acctA)), CodeUnsupportedLayout)
	t.Setenv("BATON_DESKTOP_DATA", r.data)

	sameTree(t, "the app's folder", before, tree(t, r.data))
	if _, err := r.l.Use(acctA); err != nil {
		t.Fatalf("allowed again: %v", err)
	}
}

func TestSyncedFolders(t *testing.T) {
	t.Setenv("OneDrive", `C:\Users\x\OneDrive`)
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	for p, want := range map[string]bool{
		`C:\Users\x\OneDrive\baton\desktop-logins`:           true,
		`C:\Users\x\OneDrive - Contoso\baton\desktop-logins`: true,
		`C:\Users\x\Dropbox\baton\desktop-logins`:            true,
		`D:\Google Drive\baton`:                              true,
		`C:\Users\x\AppData\Local\baton\desktop-logins`:      false,
		`C:\Users\x\OneDriveless-not\..\AppData\Local\baton`: false,
	} {
		if got := synced(p); got != want {
			t.Errorf("synced(%s) = %v", p, got)
		}
	}
}

// Only the one Claude-Data\claude_* folder holding config.json, and only
// when it is the folder baton shows sessions in.
func TestLayoutGate(t *testing.T) {
	la := t.TempDir()
	mk := func(name string, cfg bool) string {
		d := filepath.Join(la, "Claude-Data", name)
		must(t, os.MkdirAll(d, 0o700))
		if cfg {
			must(t, os.WriteFile(filepath.Join(d, "config.json"), []byte("{}"), 0o600))
		}
		return d
	}
	one := mk("claude_pzs8sxrjxfjjc", true)
	mk("claude_nothing", false) // holds no config: not the app's
	if !layoutOK(la, one) {
		t.Fatal("the one folder is refused")
	}
	if layoutOK(la, filepath.Join(la, "Claude-Data")) || layoutOK("", one) {
		t.Fatal("another folder is taken")
	}
	mk("claude_second", true)
	if layoutOK(la, one) {
		t.Fatal("two folders with a config are taken")
	}
}

// Launching never happens in a test.
func TestLaunchDoesNothingInTests(t *testing.T) {
	r := newRig(t)
	if err := r.l.Launch(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BATON_DESKTOP_DATA", "")
	if err := r.l.Launch(); err != nil {
		t.Fatal(err)
	}
	if f := family(`C:\Users\x\AppData\Local\Claude-Data\claude_pzs8sxrjxfjjc`); f != "Claude_pzs8sxrjxfjjc" {
		t.Fatalf("family %q", f)
	}
	if f := family(`C:\x\Claude-Data\claude_test`); f != "" {
		t.Fatalf("family of a test folder %q", f)
	}
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "AppxManifest.xml"), []byte(`<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10">
  <Applications>
    <Application Id="SshAskpass" Executable="app\resources\ssh-askpass.exe"/>
    <Application Id="TheApp" Executable="app\Claude.exe"/>
    <Application Id="MakeDataFolder" Executable="app\make.exe"/>
  </Applications>
</Package>`), 0o600))
	if id := appID(dir); id != "TheApp" {
		t.Fatalf("app id %q", id)
	}
	if id := appID(filepath.Join(dir, "none")); id != "Claude" {
		t.Fatalf("fallback %q", id)
	}
}
