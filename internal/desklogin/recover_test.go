package desklogin

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/config"
	"baton/internal/desktop"
	"baton/internal/fsx"

	"golang.org/x/sys/windows"
)

// steps are the places a switch from A to B can fail, in order.
func steps() []string {
	out := []string{"keep", "keep_aside", "stage"}
	for _, it := range items {
		out = append(out, "move "+it.name)
	}
	return append(out, "config", "flip", "verify")
}

// A failure at any step leaves the app exactly as it was, and no stage.
// One before the switch keeps nothing new either: the copy kept before is
// whole.
func TestRollbackAtEveryStep(t *testing.T) {
	for _, step := range steps() {
		t.Run(step, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.signIn(acctA, "A2") // what is kept of A is older
			before := tree(t, r.data)
			keptA := tree(t, filepath.Join(r.l.Store, acctA))
			r.stopAt(step, errTest)
			_, err := r.l.Use(acctB)
			want := CodeSwitchFailed
			if step == "keep" || step == "keep_aside" {
				want = CodeUnsavedCurrent
			}
			wantCode(t, "use", err, want)
			sameTree(t, "the app's folder", before, tree(t, r.data))
			r.noStage()
			if want == CodeUnsavedCurrent {
				sameTree(t, "A as kept before", keptA, tree(t, filepath.Join(r.l.Store, acctA)))
			}
			if left, _ := filepath.Glob(filepath.Join(r.l.Store, ".*")); len(left) > 0 {
				t.Fatalf("left in the store: %v", left)
			}
			r.l.failAt = nil
			if _, err := r.l.Use(acctB); err != nil {
				t.Fatalf("then: %v", err)
			}
			if desktop.Active(r.data) != acctB {
				t.Fatal("then not on B")
			}
		})
	}
}

// A switch cut off anywhere (baton killed) is seen as cut off, and undone
// by Recover or by the next switch, which then goes on. Deleting every
// kept sign-in leaves the stage alone.
func TestCutOffIsUndone(t *testing.T) {
	var at []string
	for _, it := range items[1:] {
		at = append(at, "move "+it.name) // stopped before it: the ones before moved
	}
	at = append(at, "config", "flip", "verify")
	for _, step := range at {
		for _, how := range []string{"recover", "next use", "drop all first"} {
			if how == "drop all first" && step != "verify" && step != "move "+items[1].name {
				continue
			}
			t.Run(step+", "+how, func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				before := tree(t, r.data)
				r.stopAt(step, errCrash)
				if _, err := r.l.Use(acctB); err != errCrash {
					t.Fatalf("setup: %v", err)
				}
				r.l.failAt = nil
				v, err := r.l.View()
				must(t, err)
				if !v.CutOff {
					t.Fatal("not seen as cut off")
				}
				switch how {
				case "recover":
					ok, err := r.l.Recover()
					if err != nil || !ok {
						t.Fatalf("recover: %v %v", ok, err)
					}
					sameTree(t, "the app's folder", before, tree(t, r.data))
				case "next use":
					res, err := r.l.Use(acctB)
					if err != nil || !res.Recovered || res.From != acctA {
						t.Fatalf("next use: %+v %v", res, err)
					}
					if desktop.Active(r.data) != acctB || !maps.Equal(itemsIn(t, r.data), itemFiles("B")) {
						t.Fatal("not on B after")
					}
				case "drop all first":
					if _, err := r.l.DropAll(); err != nil {
						t.Fatal(err)
					}
					if len(stages(r.data, true)) != 1 {
						t.Fatal("drop all took the stage")
					}
					r.setOn(false) // and while off
					ok, err := r.l.Recover()
					if err != nil || !ok {
						t.Fatalf("recover while off: %v %v", ok, err)
					}
					sameTree(t, "the app's folder", before, tree(t, r.data))
				}
				r.noStage()
				if v, _ := r.l.View(); v.CutOff {
					t.Fatal("still cut off")
				}
			})
		}
	}
}

// Cut off after the flip, then the app ran on B: it renewed B's tokens and
// changed a setting of its own. Recover keeps B as the app has it now, puts
// back only A's six values, and leaves the app's setting.
func TestRecoverAfterTheAppRan(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	itemsA, loginA := itemsIn(t, r.data), r.cfg().login()
	r.stopAt("verify", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	// The app ran on B.
	fresh := loginOf(acctB, "B-fresh")
	o := r.cfg()
	b := o.splice(fresh)
	b = []byte(strings.Replace(string(b), `"locale": "ko-KR"`, `"locale": "en-US"`, 1))
	r.writeCfg(b)
	r.write(filepath.Join("Network", "Cookies"), "cookies-B-fresh")

	ok, err := r.l.Recover()
	if err != nil || !ok {
		t.Fatalf("recover: %v %v", ok, err)
	}
	sameLogin(t, "B kept fresh", fresh, r.keptLogin(acctB))
	if c, _ := os.ReadFile(filepath.Join(r.l.Store, acctB, "Network", "Cookies")); string(c) != "cookies-B-fresh" {
		t.Fatalf("B's cookies kept: %q", c)
	}
	now := r.cfg()
	sameLogin(t, "A's six back", loginA, now.login())
	if !strings.Contains(r.read("config.json"), `"locale": "en-US"`) {
		t.Fatal("the app's own setting was undone")
	}
	if !maps.Equal(itemsIn(t, r.data), itemsA) {
		t.Fatal("A's items are not back")
	}
	r.noStage()
}

// A stage without a journal is a copy that never moved anything: deleted,
// never undone, and never shown as cut off.
func TestStageWithoutJournalIsDeleted(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.stopAt("stage", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	if len(stages(r.data, false)) != 1 || len(stages(r.data, true)) != 0 {
		t.Fatal("setup: no bare stage")
	}
	before := tree(t, r.data)
	if v, _ := r.l.View(); v.CutOff {
		t.Fatal("a bare stage is shown as cut off")
	}
	ok, err := r.l.Recover()
	if err != nil || ok {
		t.Fatalf("recover: %v %v", ok, err)
	}
	r.noStage()
	for p, h := range before {
		if !strings.HasPrefix(p, stagePrefix) && tree(t, r.data)[p] != h {
			t.Fatalf("%s changed", p)
		}
	}
}

// While a switch holds the lock, its stage is never shown as cut off.
func TestCutOffNotWhileLocked(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.stopAt("config", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	unlock, err := fsx.Lock(r.l.Lock)
	must(t, err)
	if v, _ := r.l.View(); v.CutOff {
		t.Fatal("cut off while the lock is held")
	}
	unlock()
	if v, _ := r.l.View(); !v.CutOff {
		t.Fatal("not cut off once free")
	}
}

// Stopped between setting the kept copy aside and putting the new one in:
// the next change of the store puts a whole copy in place, the new one when
// it is whole, else the one set aside.
func TestStopBetweenAsideAndIn(t *testing.T) {
	for _, whole := range []bool{true, false} {
		name := "the new copy is whole"
		if !whole {
			name = "the new copy is not"
		}
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			oldA := tree(t, filepath.Join(r.l.Store, acctA))
			r.signIn(acctA, "A2")
			r.stopAt("keep_aside", errCrash)
			if _, err := r.l.Use(acctB); err != errCrash {
				t.Fatalf("setup: %v", err)
			}
			r.l.failAt = nil
			if exists(filepath.Join(r.l.Store, acctA)) {
				t.Fatal("setup: A is in place")
			}
			keeps, _ := filepath.Glob(filepath.Join(r.l.Store, ".keep-"+acctA+"-*"))
			if len(keeps) != 1 {
				t.Fatal("setup: no keep")
			}
			if !whole {
				must(t, os.Remove(filepath.Join(keeps[0], "meta.json")))
			}
			if err := r.l.Drop(acctC); Code(err) != CodeNotKept { // any change of the store sweeps first
				t.Fatal(err)
			}
			if why := checkFolder(filepath.Join(r.l.Store, acctA), acctA); why != "" {
				t.Fatalf("A after the sweep: %s", why)
			}
			if !whole {
				sameTree(t, "A as kept before", oldA, tree(t, filepath.Join(r.l.Store, acctA)))
			} else {
				sameLogin(t, "A as kept now", loginOf(acctA, "A2"), r.keptLogin(acctA))
			}
			if left, _ := filepath.Glob(filepath.Join(r.l.Store, ".*")); len(left) > 0 {
				t.Fatalf("left: %v", left)
			}
		})
	}
}

// TurnOff deletes every kept sign-in and the acknowledgement, takes the
// lock only once (no wait on itself), and leaves the app as it is.
func TestTurnOff(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	before := tree(t, r.data)
	done := make(chan struct{})
	var n int
	var err error
	go func() {
		n, err = r.l.turnOff()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("TurnOff waits on itself")
	}
	if err != nil || n != 2 || exists(r.l.Store) {
		t.Fatalf("turn off: %d %v", n, err)
	}
	c, err := config.Load(r.paths.Config())
	must(t, err)
	if c.DesktopLogins || c.DesktopLoginsAcknowledged != 0 {
		t.Fatalf("config: %+v", c)
	}
	sameTree(t, "the app's folder", before, tree(t, r.data))
	// Through the paths, as the window and the CLI call it.
	if n, err := TurnOff(r.paths); err != nil || n != 0 {
		t.Fatalf("again: %d %v", n, err)
	}
	if err := TurnOn(r.paths, false); err != config.ErrNeedsDesktopAcknowledgement {
		t.Fatalf("on again without the notice: %v", err)
	}
	if err := TurnOn(r.paths, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(r.paths.Config()); !c.DesktopLoginsOn() {
		t.Fatal("not on")
	}
}

// A TurnOff while a switch waited for the lock wins: the switch reads the
// setting again once it has the lock, and the store stays deleted.
func TestTurnOffWhileAUseWaits(t *testing.T) {
	r := newRig(t)
	_, err := r.l.Blank()
	must(t, err)
	r.signIn(acctB, "B")
	before := tree(t, r.data)
	r.l.afterGate = func() {
		if _, err := r.l.turnOff(); err != nil {
			t.Error(err)
		}
	}
	wantCode(t, "use", useErr(r.l.Use(acctA)), CodeOff)
	if exists(r.l.Store) {
		t.Fatal("the store came back")
	}
	sameTree(t, "the app's folder", before, tree(t, r.data))
}

// While another switch, or baton's desktop lists, hold their lock, a
// switch gives up within its wait and changes nothing in the app.
func TestBusy(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	before := tree(t, r.data)
	unlock, err := fsx.Lock(r.l.Lock)
	must(t, err)
	wantCode(t, "use", useErr(r.l.Use(acctB)), CodeBusy)
	wantCode(t, "blank", useErr(r.l.Blank()), CodeBusy)
	wantCode(t, "rename", r.l.Rename(acctB, "x"), CodeBusy)
	wantCode(t, "drop", r.l.Drop(acctB), CodeBusy)
	_, err = r.l.DropAll()
	wantCode(t, "drop all", err, CodeBusy)
	_, err = r.l.turnOff()
	wantCode(t, "turn off", err, CodeBusy)
	unlock()

	unlock, err = fsx.Lock(r.l.DeskLock)
	must(t, err)
	wantCode(t, "use while baton's desktop lists are locked", useErr(r.l.Use(acctB)), CodeBusy)
	unlock()
	sameTree(t, "the app's folder", before, tree(t, r.data))
	r.noStage()
	if _, err := r.l.Use(acctB); err != nil {
		t.Fatalf("free: %v", err)
	}
}

// A switch that cannot take baton's desktop lock (another baton brings its
// lists in line) has moved nothing: its stage is only a copy. When another
// program holds the journal open just then, the stage is marked done, not
// left pending: View and CutOffNow do not take it for a switch cut off, and
// Recover changes nothing, so an item the app made since (WebStorage, which
// A did not have) stays.
func TestBusyWithItsJournalHeld(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	was := moveWait
	moveWait = 300 * time.Millisecond
	t.Cleanup(func() { moveWait = was })
	must(t, os.RemoveAll(filepath.Join(r.data, "WebStorage")))
	unlock, err := fsx.Lock(r.l.DeskLock)
	must(t, err)
	// The journal is held as soon as it is there, before the switch gives
	// up on the lock (r.l.wait).
	held, done := make(chan func(), 1), make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				held <- nil
				return
			default:
			}
			for _, s := range stages(r.data, false) {
				u, _ := windows.UTF16PtrFromString(filepath.Join(s, "journal.json"))
				h, err := windows.CreateFile(u, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
					windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
				if err == nil {
					var once sync.Once
					held <- func() { once.Do(func() { windows.CloseHandle(h) }) }
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()
	_, err = r.l.Use(acctB)
	close(done)
	unlock()
	release := <-held
	if release == nil {
		t.Fatal("setup: the journal was never held")
	}
	t.Cleanup(release)
	wantDetail(t, "use", err, CodeBusy, "desktop")
	if desktop.Active(r.data) != acctA || r.read(filepath.Join("Network", "Cookies")) != "cookies-A" {
		t.Fatal("the app is no longer on A")
	}
	if v, _ := r.l.View(); v.CutOff || v.Leftover != 1 {
		t.Fatalf("view: %+v", v)
	}
	if cut, err := r.l.CutOffNow(); cut || err != nil {
		t.Fatalf("cut off now: %v %v", cut, err)
	}
	// The app then made WebStorage of its own.
	r.write(filepath.Join("WebStorage", "QuotaManager"), "qm-A")
	before := outsideStages(tree(t, r.data))
	release()
	if ok, err := r.l.Recover(); ok || err != nil {
		t.Fatalf("recover: %v %v", ok, err)
	}
	sameTree(t, "the app's folder", before, tree(t, r.data))
	r.noStage()
}

// TurnOn makes the store, with permissions of its own, before it turns
// switching on; TurnOff deletes it again.
func TestTurnOnMakesTheStore(t *testing.T) {
	r := newRig(t)
	if _, err := r.l.turnOff(); err != nil {
		t.Fatal(err)
	}
	if exists(r.l.Store) {
		t.Fatal("setup: the store is there")
	}
	must(t, TurnOn(r.paths, true))
	sd, err := windows.GetNamedSecurityInfo(r.l.Store, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	must(t, err)
	if c, _, err := sd.Control(); err != nil || c&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("the store's permissions: %s", sd.String())
	}
	if n, err := TurnOff(r.paths); err != nil || n != 0 || exists(r.l.Store) {
		t.Fatalf("turn off: %d %v, store there: %v", n, err, exists(r.l.Store))
	}
}

// denyPermissionChange makes dir a folder whose permissions its owner
// cannot change, as on a drive or share where baton cannot give the store
// permissions of its own: OWNER RIGHTS is denied WRITE_DAC (passed on to
// what is made inside, with inherit), and nothing else is taken away.
func denyPermissionChange(t *testing.T, dir string, inherit bool) {
	t.Helper()
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	must(t, err)
	flags := ""
	if inherit {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:(D;" + flags + ";WD;;;OW)(A;OICI;FA;;;" + u.User.Sid.String() + ")(A;OICI;FA;;;SY)")
	must(t, err)
	dacl, _, err := sd.DACL()
	must(t, err)
	must(t, windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil))
}

// A store that cannot get permissions of its own could never keep a
// sign-in: TurnOn refuses then, with the reason, and switching stays off.
// A store folder it made for that goes again.
func TestTurnOnRefusedWithoutStorePermissions(t *testing.T) {
	for _, how := range []string{"store made before", "store made now"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			if _, err := r.l.turnOff(); err != nil {
				t.Fatal(err)
			}
			if how == "store made before" {
				must(t, os.MkdirAll(r.l.Store, 0o700))
				denyPermissionChange(t, r.l.Store, false)
			} else {
				denyPermissionChange(t, r.paths.Root, true)
			}
			wantDetail(t, "turn on", TurnOn(r.paths, true), CodeUnsavedCurrent, "store_permissions")
			if c, _ := config.Load(r.paths.Config()); c.DesktopLoginsOn() {
				t.Fatal("turned on")
			}
			if got := exists(r.l.Store); got != (how == "store made before") {
				t.Fatalf("the store is there: %v", got)
			}
		})
	}
}

// CutOffNow sees a switch that was cut off also while another baton holds
// the lock for a moment (its own look, as View's): it waits for the lock,
// where View's look sees nothing. Held longer than its wait, it says busy.
func TestCutOffNowWaitsForTheLock(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	if cut, err := r.l.CutOffNow(); cut || err != nil {
		t.Fatalf("nothing cut off: %v %v", cut, err)
	}
	r.stopAt("config", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	unlock, err := fsx.Lock(r.l.Lock)
	must(t, err)
	if v, _ := r.l.View(); v.CutOff {
		t.Fatal("setup: View saw it while the lock was held")
	}
	_, err = r.l.CutOffNow()
	wantCode(t, "held longer than its wait", err, CodeBusy)
	r.l.wait = 5 * time.Second
	go func() {
		time.Sleep(100 * time.Millisecond)
		unlock()
	}()
	if cut, err := r.l.CutOffNow(); !cut || err != nil {
		t.Fatalf("held for a moment: %v %v", cut, err)
	}
	// No folder of the app's at all: no switch of baton's to be cut off.
	t.Setenv("LOCALAPPDATA", filepath.Join(r.root, "none"))
	r.l.dataFn = func() string { return "" }
	if cut, err := r.l.CutOffNow(); cut || err != nil {
		t.Fatalf("no app folder: %v %v", cut, err)
	}
}
