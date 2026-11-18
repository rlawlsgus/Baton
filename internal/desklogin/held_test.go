package desklogin

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/desktop"

	"golang.org/x/sys/windows"
)

// Another program holds open, without sharing it for reading (share mode
// 0), a file restore and Settle go by: config.json, the journal, or the
// config.json the switch read (stage\old). Such a file says nothing of where
// the app is, so Recover and Settle change nothing and say to try again
// (dl_switch_unrestored). Taken for a file that is gone, they left A's
// tokens over B's cookies and deleted A's own, or put the old items under a
// sign-in made since and deleted its cookies. Once the file is let go, they
// work as they do when nothing is held.
func TestHeldWithoutReadSharingIsTriedAgain(t *testing.T) {
	cookies := filepath.Join("Network", "Cookies")
	// tokenOf is the token the app's config.json holds ("" none).
	tokenOf := func(r *rig) string {
		for _, k := range r.cfg().login() {
			if k.key == "oauth:tokenCache" {
				var s string
				jsonString(k.raw, &s)
				return s
			}
		}
		return ""
	}
	cases := []struct {
		name  string
		setup func(r *rig)
		act   string // what then works
		check func(r *rig)
	}{
		{"cut before the flip", func(r *rig) { r.cutBeforeFlip() }, "settle", func(r *rig) {
			if tokenOf(r) != "sealed-A-1" || r.read(cookies) != "cookies-A" {
				r.t.Fatalf("not A whole: %q with %q", tokenOf(r), r.read(cookies))
			}
		}},
		{"cut before the flip, a setting changed since", func(r *rig) {
			r.cutBeforeFlip()
			r.writeCfg([]byte(strings.Replace(r.read("config.json"), `"locale": "ko-KR"`, `"locale": "en-US"`, 1)))
		}, "settle", func(r *rig) {
			if tokenOf(r) != "sealed-A-1" || r.read(cookies) != "cookies-A" || !hasLocale(r, "en-US") {
				r.t.Fatalf("not A whole, as the app set it: %q with %q", tokenOf(r), r.read(cookies))
			}
		}},
		{"from its own Log out, cut before the flip, signed in to A again", func(r *rig) {
			o := r.cfg()
			var out []kv
			for _, k := range o.login() {
				if k.key == "oauth:tokenCache" || k.key == "oauth:tokenCacheV2" {
					k.raw = []byte(`""`)
				}
				out = append(out, k)
			}
			r.writeCfg(o.splice(out))
			r.clearItems()
			r.write(cookies, "cookies-out")
			r.cutBeforeFlip()
			r.signIn(acctA, "An")
		}, "recover", func(r *rig) {
			if tokenOf(r) != "" || r.read(cookies) != "cookies-out" {
				r.t.Fatalf("the signed-out app is not back: %q with %q", tokenOf(r), r.read(cookies))
			}
			sameLogin(r.t, "An kept", loginOf(acctA, "An"), r.keptLogin(acctA))
			if got := readKept(r.t, r, acctA, cookies); got != "cookies-An" {
				r.t.Fatalf("An's kept cookies: %q", got)
			}
		}},
		{"cut while moving, signed in to C since", func(r *rig) {
			r.stopAt("config", errCrash)
			if _, err := r.l.Use(acctB); err != errCrash {
				r.t.Fatalf("setup: %v", err)
			}
			r.l.failAt = nil
			r.signIn(acctC, "C")
		}, "recover", func(r *rig) {
			if tokenOf(r) != "sealed-A-1" || r.read(cookies) != "cookies-A" {
				r.t.Fatalf("not A whole: %q with %q", tokenOf(r), r.read(cookies))
			}
			sameLogin(r.t, "C kept", loginOf(acctC, "C"), r.keptLogin(acctC))
			if got := readKept(r.t, r, acctC, cookies); got != "cookies-C" {
				r.t.Fatalf("C's kept cookies: %q", got)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			was := moveWait
			moveWait = 300 * time.Millisecond
			t.Cleanup(func() { moveWait = was })
			r.keptAll()
			c.setup(r)
			s := stages(r.data, true)
			if len(s) != 1 {
				t.Fatalf("setup: %d stages", len(s))
			}
			data, store := tree(t, r.data), tree(t, r.l.Store)
			for _, held := range []string{filepath.Join(r.data, "config.json"), filepath.Join(s[0], "journal.json"),
				filepath.Join(s[0], "old", "config.json")} {
				for _, act := range []string{"recover", "settle"} {
					release := holdExclusive(t, held)
					if release == nil {
						t.Fatalf("setup: no %s", held)
					}
					var err error
					if act == "recover" {
						_, err = r.l.Recover()
					} else {
						_, err = r.l.Settle()
					}
					release()
					rel, _ := filepath.Rel(r.data, held)
					wantCode(t, act+" while "+rel+" is held", err, CodeSwitchUnrestored)
					sameTree(t, "the app's folder after "+act+" while "+rel+" is held", data, tree(t, r.data))
					sameTree(t, "the store after "+act+" while "+rel+" is held", store, tree(t, r.l.Store))
				}
			}
			var ok bool
			var err error
			if c.act == "recover" {
				ok, err = r.l.Recover()
			} else {
				ok, err = r.l.Settle()
			}
			if err != nil || !ok {
				t.Fatalf("%s once let go: %v %v", c.act, ok, err)
			}
			r.noStage()
			c.check(r)
		})
	}
}

// readKept is the file rel of the sign-in kept for acct.
func readKept(t *testing.T, r *rig, acct, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.l.Store, acct, rel))
	must(t, err)
	return string(b)
}

// hasLocale: the app's config.json holds the locale setting loc.
func hasLocale(r *rig, loc string) bool {
	m, ok := r.cfg().get("locale")
	if !ok {
		return false
	}
	var s string
	return jsonString(r.cfg().raw(m), &s) && s == loc
}

// The app holds A's tokens under B's name (a stale lastKnownAccountUuid).
// While A's kept keys.json is held without sharing it for reading, whose
// tokens these are cannot be told: a Blank refuses (dl_unsaved_current,
// naming A's kept sign-in) and changes nothing, instead of keeping A's
// tokens as B's sign-in and emptying the app. Once it is let go, it says why
// (dl_damaged).
func TestTokensOfAnotherWhileItsKeysAreHeld(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	l := loginOf(acctB, "B")
	l[1] = loginOf(acctA, "A")[1] // A's sealed tokens, named B
	r.signIn(acctB, "B")
	r.writeCfg(appCfg(l, "\n"))
	before, store := tree(t, r.data), tree(t, r.l.Store)
	release := holdExclusive(t, filepath.Join(r.l.Store, acctA, "keys.json"))
	err = useErr(r.l.Blank())
	release()
	wantDetail(t, "blank while A's keys.json is held", err, CodeUnsavedCurrent, "kept_unreadable:work")
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
	wantCode(t, "blank", useErr(r.l.Blank()), CodeDamaged)
}

// Another program holds baton's own config.json without sharing it for
// reading, so whether switching is on cannot be told now. That is never
// taken for "off": Settle would then not keep from the stage the only whole
// copy of the sign-in the switch moved out (its kept copy is not whole),
// and would delete it with the stage; Recover would say the sign-in used
// since cannot be kept (dl_used_since), which sends the user to that very
// Settle. Both change nothing and say to try again; once it is let go,
// they work as they do when nothing is held.
func TestBatonsConfigHeldIsTriedAgain(t *testing.T) {
	cookies := filepath.Join("Network", "Cookies")
	for _, act := range []string{"settle", "recover"} {
		t.Run(act, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.cutAfterFlip(acctB)
			if act == "settle" {
				must(t, os.Remove(filepath.Join(r.l.Store, acctA, cookies))) // A's kept copy is not whole
			} else {
				r.signIn(acctC, "C") // used since on C, which Recover keeps first
			}
			data, store := tree(t, r.data), tree(t, r.l.Store)
			release := holdExclusive(t, r.paths.Config())
			var err error
			if act == "settle" {
				_, err = r.l.Settle()
			} else {
				_, err = r.l.Recover()
			}
			release()
			wantCode(t, act+" while baton's config.json is held", err, CodeSwitchUnrestored)
			sameTree(t, "the app's folder", data, tree(t, r.data))
			sameTree(t, "the store", store, tree(t, r.l.Store))
			var ok bool
			if act == "settle" {
				ok, err = r.l.Settle()
			} else {
				ok, err = r.l.Recover()
			}
			if err != nil || !ok {
				t.Fatalf("%s once let go: %v %v", act, ok, err)
			}
			r.noStage()
			if act == "settle" {
				if why := checkFolder(filepath.Join(r.l.Store, acctA), acctA); why != "" {
					t.Fatalf("A kept: %s", why)
				}
				if got := readKept(t, r, acctA, cookies); got != "cookies-A" {
					t.Fatalf("A's kept cookies: %q", got)
				}
				return
			}
			if desktop.Active(r.data) != acctA || r.read(cookies) != "cookies-A" {
				t.Fatalf("not A whole: on %s with %q", desktop.Active(r.data), r.read(cookies))
			}
			sameLogin(t, "C kept", loginOf(acctC, "C"), r.keptLogin(acctC))
		})
	}
}

// Another program holds the app's data folder itself open without sharing
// it (share mode 0), so the stages in it cannot be listed. That is never
// taken for "no switch was cut off": uninstall, which asks CutOffNow, would
// then delete every kept sign-in and leave the app half switched (A's
// tokens with B's cookies, A's own items in a stage nothing settles), and a
// Blank would keep that half switched app as A's sign-in. CutOffNow fails,
// and Use, Blank, Recover and Settle change nothing and say to try again;
// once it is let go, Recover puts A back whole.
func TestAppFolderHeldIsTriedAgain(t *testing.T) {
	cookies := filepath.Join("Network", "Cookies")
	r := newRig(t)
	r.keptAll()
	r.cutBeforeFlip()
	data, store := tree(t, r.data), tree(t, r.l.Store)
	release := holdFolder(t, r.data)
	if cut, err := r.l.CutOffNow(); err == nil {
		t.Fatalf("CutOffNow while the app's folder is held: %v, and no error", cut)
	}
	wantCode(t, "blank", useErr(r.l.Blank()), CodeSwitchUnrestored)
	wantCode(t, "use", useErr(r.l.Use(acctB)), CodeSwitchUnrestored)
	wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
	_, err := r.l.Settle()
	wantCode(t, "settle", err, CodeSwitchUnrestored)
	release()
	sameTree(t, "the app's folder", data, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))
	if cut, err := r.l.CutOffNow(); !cut || err != nil {
		t.Fatalf("CutOffNow once let go: %v %v", cut, err)
	}
	if ok, err := r.l.Recover(); !ok || err != nil {
		t.Fatalf("recover once let go: %v %v", ok, err)
	}
	r.noStage()
	if desktop.Active(r.data) != acctA || r.read(cookies) != "cookies-A" {
		t.Fatalf("not A whole: on %s with %q", desktop.Active(r.data), r.read(cookies))
	}
	if got := readKept(t, r, acctA, cookies); got != "cookies-A" {
		t.Fatalf("A's kept cookies: %q", got)
	}
}

// Another program holds Claude-Data, the folder the app's folder is in,
// without sharing it, while a switch is cut off; the app's folders are read
// for real (no BATON_DESKTOP_DATA). Finding the app's folder lists
// Claude-Data, so while it is held baton finds none, or the older
// %APPDATA%\Claude. That is never taken for "no folder of the app's, so no
// switch cut off": uninstall would then delete every kept sign-in, A's
// among them, and leave the app half switched (A's tokens with B's
// cookies, A's own items in a stage nothing settles any more). CutOffNow
// fails; once it is let go, it sees the switch, and Recover puts A back
// whole.
func TestAppsParentFolderHeldIsTriedAgain(t *testing.T) {
	cookies := filepath.Join("Network", "Cookies")
	for _, found := range []string{"none", "the older folder"} {
		t.Run("finds "+found, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.cutBeforeFlip()
			parent := filepath.Join(r.root, "Claude-Data")
			older := filepath.Join(r.root, "roaming", "Claude")
			must(t, os.MkdirAll(older, 0o700))
			must(t, os.WriteFile(filepath.Join(older, "config.json"), appCfg(nil, "\n"), 0o600))
			t.Setenv("BATON_DESKTOP_DATA", "")
			t.Setenv("LOCALAPPDATA", r.root)
			// As desktop.DataDir finds it: by listing Claude-Data first.
			r.l.dataFn = func() string {
				if _, err := os.ReadDir(parent); err == nil {
					return r.data
				}
				if found == "none" {
					return ""
				}
				return older
			}
			if cut, err := r.l.CutOffNow(); !cut || err != nil {
				t.Fatalf("setup: %v %v", cut, err)
			}
			release := holdFolder(t, parent)
			if cut, err := r.l.CutOffNow(); err == nil {
				t.Fatalf("CutOffNow while Claude-Data is held: %v, and no error", cut)
			}
			release()
			if cut, err := r.l.CutOffNow(); !cut || err != nil {
				t.Fatalf("CutOffNow once let go: %v %v", cut, err)
			}
			if ok, err := r.l.Recover(); !ok || err != nil {
				t.Fatalf("recover once let go: %v %v", ok, err)
			}
			r.noStage()
			if desktop.Active(r.data) != acctA || r.read(cookies) != "cookies-A" {
				t.Fatalf("not A whole: on %s with %q", desktop.Active(r.data), r.read(cookies))
			}
		})
	}
}

// holdFolder opens the folder p as another program may: sharing it with no
// one, so it cannot be listed until release.
func holdFolder(t *testing.T, p string) (release func()) {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	must(t, err)
	h, err := windows.CreateFile(u, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	must(t, err)
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(release)
	return release
}

// Another account's kept keys.json cannot be read, and never will be (a
// folder is in its place, or its permissions shut the user out). Whose
// tokens the app has cannot be told, so a Blank, a Use of any other
// account, and a Recover that would keep the app's sign-in first change
// nothing; they say which kept sign-in is at fault (by its label), since
// trying again does not help, and forgetting it does.
func TestKeptKeysThatCannotBeReadAreNamed(t *testing.T) {
	for _, act := range []string{"switch", "recover"} {
		t.Run(act, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.signIn(acctC, "C")
			_, err := r.l.Use(acctA) // C kept too
			must(t, err)
			if act == "recover" {
				r.cutAfterFlip(acctC)
				r.signIn(acctC, "C2") // used since on C, which Recover keeps first
			}
			keys := filepath.Join(r.l.Store, acctB, "keys.json")
			must(t, os.Remove(keys))
			must(t, os.Mkdir(keys, 0o700))
			data, store := tree(t, r.data), tree(t, r.l.Store)
			const want = "kept_unreadable:home" // B is baton's account home
			if act == "recover" {
				wantDetail(t, "recover", recErr(r.l.Recover()), CodeUnsavedCurrent, want)
			} else {
				wantDetail(t, "blank", useErr(r.l.Blank()), CodeUnsavedCurrent, want)
				wantDetail(t, "use C", useErr(r.l.Use(acctC)), CodeUnsavedCurrent, want)
				wantCode(t, "use B", useErr(r.l.Use(acctB)), CodeDamaged)
			}
			sameTree(t, "the app's folder", data, tree(t, r.data))
			sameTree(t, "the store", store, tree(t, r.l.Store))
			must(t, r.l.Drop(acctB))
			if act == "recover" {
				if ok, err := r.l.Recover(); !ok || err != nil {
					t.Fatalf("recover once B is forgotten: %v %v", ok, err)
				}
				sameLogin(t, "C kept", loginOf(acctC, "C2"), r.keptLogin(acctC))
				return
			}
			if _, err := r.l.Use(acctC); err != nil {
				t.Fatalf("use C once B is forgotten: %v", err)
			}
		})
	}
}
