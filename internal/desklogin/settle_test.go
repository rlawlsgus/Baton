package desklogin

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"baton/internal/desktop"

	"golang.org/x/sys/windows"
)

// ranOn makes the app look as if it ran since a switch was cut off: it
// wrote these six values (every other setting stays) and these cookies.
func (r *rig) ranOn(login []kv, cookies string) {
	r.t.Helper()
	r.writeCfg(r.cfg().splice(login))
	r.write(filepath.Join("Network", "Cookies"), cookies)
}

// cutAfterFlip is a Use (to "B") or a Blank (to "") cut off once config.json
// was written: baton killed before it looked that all is as meant.
func (r *rig) cutAfterFlip(to string) {
	r.t.Helper()
	r.stopAt("verify", errCrash)
	var err error
	if to == "" {
		_, err = r.l.Blank()
	} else {
		_, err = r.l.Use(to)
	}
	if err != errCrash {
		r.t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
}

func wantDetail(t *testing.T, what string, err error, code, detail string) {
	t.Helper()
	var de *desktop.Error
	if !errors.As(err, &de) || de.Code != code || de.Detail != detail {
		t.Fatalf("%s: got %v, want %s (%s)", what, err, code, detail)
	}
}

// outsideStages is tree without the switches' stages.
func outsideStages(in map[string]string) map[string]string {
	out := map[string]string{}
	for p, h := range in {
		if !strings.HasPrefix(p, stagePrefix) {
			out[p] = h
		}
	}
	return out
}

// Cut off after the flip, the app then ran on B and renewed B's sign-in.
// Turned off, baton keeps nothing, so B's renewed sign-in cannot be kept:
// Recover changes nothing and says so, instead of putting A back over it.
// Settle leaves the app on B as it is. Uninstall, which settles and then
// deletes every kept sign-in, leaves it on B too, as it says.
func TestRecoverNeverDropsTheSignInUsedSince(t *testing.T) {
	for _, how := range []string{"turned off", "uninstall"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.cutAfterFlip(acctB)
			fresh := loginOf(acctB, "B-fresh")
			r.ranOn(fresh, "cookies-B-fresh")
			if how == "turned off" {
				if _, err := r.l.turnOff(); err != nil {
					t.Fatal(err)
				}
				before := tree(t, r.data)
				wantCode(t, "recover while off", recErr(r.l.Recover()), CodeUsedSince)
				sameTree(t, "the app's folder after recover", before, tree(t, r.data))
				if v, _ := r.l.View(); !v.CutOff {
					t.Fatal("no longer shown as cut off")
				}
			}
			ok, err := r.l.Settle()
			if err != nil || !ok {
				t.Fatalf("settle: %v %v", ok, err)
			}
			if how == "uninstall" {
				if _, err := r.l.DropAll(); err != nil {
					t.Fatal(err)
				}
			}
			if desktop.Active(r.data) != acctB {
				t.Fatal("not on B")
			}
			sameLogin(t, "B as the app renewed it", fresh, r.cfg().login())
			if c := r.read(filepath.Join("Network", "Cookies")); c != "cookies-B-fresh" {
				t.Fatalf("cookies: %q", c)
			}
			r.noStage()
			if v, _ := r.l.View(); v.CutOff || v.Leftover != 0 {
				t.Fatalf("view: %+v", v)
			}
		})
	}
}

// Before the flip the app still has its own config.json: Settle puts its
// items back, as Recover does, so it stays on the account it is on.
func TestSettleBeforeTheFlipPutsBack(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	before := tree(t, r.data)
	r.stopAt("config", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	ok, err := r.l.Settle()
	if err != nil || !ok {
		t.Fatalf("settle: %v %v", ok, err)
	}
	sameTree(t, "the app's folder", before, tree(t, r.data))
	r.noStage()
}

// Cut off after the flip of a Blank, the app was then signed in to C and
// quit before it named the account. That sign-in is never thrown away:
// Recover, Use and Blank change nothing and say the app has not named it
// yet, and tokens that are another kept account's are refused as they are
// by a switch. Once the app names C, Recover keeps C and puts A back.
func TestRecoverKeepsASignInNotNamedYet(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.cutAfterFlip("")
	c := loginOf(acctC, "C")
	r.ranOn(append(c[:2:2], c[3:]...), "cookies-C") // no lastKnownAccountUuid
	before, store := tree(t, r.data), tree(t, r.l.Store)
	wantDetail(t, "recover", recErr(r.l.Recover()), CodeUnsavedCurrent, "no_account")
	wantDetail(t, "use", useErr(r.l.Use(acctB)), CodeUnsavedCurrent, "no_account")
	wantDetail(t, "blank", useErr(r.l.Blank()), CodeUnsavedCurrent, "no_account")
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))

	stale := loginOf(acctC, "C")
	stale[0], stale[1] = r.keptLogin(acctB)[0], r.keptLogin(acctB)[1]
	r.ranOn(stale, "cookies-C")
	before = tree(t, r.data)
	wantDetail(t, "recover with B's tokens named C", recErr(r.l.Recover()), CodeDamaged, "tokens_of_another")
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))

	r.ranOn(c, "cookies-C")
	ok, err := r.l.Recover()
	if err != nil || !ok {
		t.Fatalf("recover once named: %v %v", ok, err)
	}
	sameLogin(t, "C kept", c, r.keptLogin(acctC))
	if b, _ := os.ReadFile(filepath.Join(r.l.Store, acctC, "Network", "Cookies")); string(b) != "cookies-C" {
		t.Fatalf("C's cookies kept: %q", b)
	}
	sameLogin(t, "A back", loginOf(acctA, "A"), r.cfg().login())
	if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
		t.Fatal("A's items are not back")
	}
	r.noStage()
}

// Cut off after the flip, then config.json became a shape baton does not
// know (an app update added a token key), or went: Recover cannot undo
// that, so Recover, Use and Blank refuse, and Launch would too. Settle
// ends it, on or off, and leaves the app as it is. While on, the sign-in
// the switch moved out is kept from the stage when its kept copy is not
// whole.
func TestSettleEndsWhatRecoverCannotUndo(t *testing.T) {
	for _, how := range []string{"unknown key", "unknown key, off", "config gone", "kept copy damaged"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.cutAfterFlip(acctB)
			switch how {
			case "config gone":
				must(t, os.Remove(filepath.Join(r.data, "config.json")))
			default:
				r.writeCfg(r.cfg().splice(append(loginOf(acctB, "B-fresh"), kv{"oauth:tokenCacheV3", []byte(`"x"`)})))
			}
			off := strings.HasSuffix(how, "off")
			if off {
				if _, err := r.l.turnOff(); err != nil {
					t.Fatal(err)
				}
			}
			if how == "kept copy damaged" {
				must(t, os.Remove(filepath.Join(r.l.Store, acctA, "meta.json")))
			}
			before := tree(t, r.data)
			wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
			if !off {
				wantCode(t, "use", useErr(r.l.Use(acctA)), CodeSwitchUnrestored)
				wantCode(t, "blank", useErr(r.l.Blank()), CodeSwitchUnrestored)
			}
			sameTree(t, "the app's folder", before, tree(t, r.data))

			ok, err := r.l.Settle()
			if err != nil || !ok {
				t.Fatalf("settle: %v %v", ok, err)
			}
			sameTree(t, "the app's folder, as it was", outsideStages(before), tree(t, r.data))
			r.noStage()
			if v, _ := r.l.View(); v.CutOff || v.Leftover != 0 {
				t.Fatalf("view: %+v", v)
			}
			if ok, err := r.l.Recover(); ok || err != nil {
				t.Fatalf("recover after: %v %v", ok, err)
			}
			if off {
				if exists(r.l.Store) {
					t.Fatal("kept something while off")
				}
				return
			}
			if why := checkFolder(filepath.Join(r.l.Store, acctA), acctA); why != "" {
				t.Fatalf("A kept: %s", why)
			}
			sameLogin(t, "A kept", loginOf(acctA, "A"), r.keptLogin(acctA))
			if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, acctA)), itemFiles("A")) {
				t.Fatal("A's kept items")
			}
		})
	}
}

// A journal that cannot be read gives Recover nothing to go by. Settle
// still ends it: while config.json is as the switch read it, what was moved
// out goes back; once the flip landed, the app is left as it is.
func TestSettleWithAJournalItCannotRead(t *testing.T) {
	for _, step := range []string{"move " + items[2].name, "verify"} {
		t.Run(step, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			itemsA := itemsIn(t, r.data)
			r.stopAt(step, errCrash)
			if _, err := r.l.Use(acctB); err != errCrash {
				t.Fatalf("setup: %v", err)
			}
			r.l.failAt = nil
			s := stages(r.data, true)
			if len(s) != 1 {
				t.Fatal("setup: no stage")
			}
			must(t, os.WriteFile(filepath.Join(s[0], "journal.json"), []byte("{"), 0o600))
			wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
			before := outsideStages(tree(t, r.data))
			ok, err := r.l.Settle()
			if err != nil || !ok {
				t.Fatalf("settle: %v %v", ok, err)
			}
			r.noStage()
			if step == "verify" {
				sameTree(t, "the app's folder, as it was", before, tree(t, r.data))
				if desktop.Active(r.data) != acctB {
					t.Fatal("not left on B")
				}
				return
			}
			if !maps.Equal(itemsIn(t, r.data), itemsA) || desktop.Active(r.data) != acctA {
				t.Fatal("A is not back")
			}
		})
	}
}

// Cut off while items move, config.json plays no part: Recover puts the
// items back even when config.json is gone.
func TestRecoverWhileMovingNeedsNoConfig(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	itemsA := itemsIn(t, r.data)
	r.stopAt("move "+items[2].name, errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	must(t, os.Remove(filepath.Join(r.data, "config.json")))
	ok, err := r.l.Recover()
	if err != nil || !ok {
		t.Fatalf("recover: %v %v", ok, err)
	}
	if !maps.Equal(itemsIn(t, r.data), itemsA) {
		t.Fatal("A's items are not back")
	}
	r.noStage()
}

// A switch stopped while it filled its stage (no journal yet) leaves only
// copies in the app's folder: config.json as it was, tokens and all, and
// B's kept files. View counts them; turning off, DropAll and Tidy delete
// them, while the app runs too.
func TestCopiesLeftInTheAppAreDeleted(t *testing.T) {
	for _, how := range []string{"turn off", "drop all", "tidy"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.stopAt("stage", errCrash)
			if _, err := r.l.Use(acctB); err != errCrash {
				t.Fatalf("setup: %v", err)
			}
			r.l.failAt = nil
			if len(stages(r.data, false)) != 1 {
				t.Fatal("setup: no stage")
			}
			if v, _ := r.l.View(); v.CutOff || v.Leftover != 1 {
				t.Fatalf("view: %+v", v)
			}
			before := outsideStages(tree(t, r.data))
			t.Setenv("BATON_DESKTOP_RUNNING", "1")
			var err error
			switch how {
			case "turn off":
				_, err = r.l.turnOff()
			case "drop all":
				_, err = r.l.DropAll()
			case "tidy":
				var n int
				if n, err = r.l.Tidy(); n != 1 {
					t.Fatalf("tidy: %d", n)
				}
			}
			must(t, err)
			r.noStage()
			sameTree(t, "the app's folder", before, tree(t, r.data))
			if v, _ := r.l.View(); v.Leftover != 0 {
				t.Fatalf("view after: %+v", v)
			}
		})
	}
}

// When the journal of a switch that is done cannot be deleted (another
// program holds it open), the stage is marked done instead: the switch is
// reported done, never shown as cut off, never undone after, and its stage
// is deleted once it can be.
func TestDoneSwitchWhoseJournalIsHeld(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	was := moveWait
	moveWait = 300 * time.Millisecond
	t.Cleanup(func() { moveWait = was })
	var held windows.Handle
	t.Cleanup(func() {
		if held != 0 {
			windows.CloseHandle(held)
		}
	})
	r.l.failAt = func(step string) error {
		if step != "commit" {
			return nil
		}
		s := stages(r.data, true)
		if len(s) != 1 {
			return errors.New("no stage at commit")
		}
		p, _ := windows.UTF16PtrFromString(filepath.Join(s[0], "journal.json"))
		h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
			windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		held = h
		return err
	}
	res, err := r.l.Use(acctB)
	if err != nil || res.To != acctB || held == 0 {
		t.Fatalf("use: %+v %v", res, err)
	}
	r.l.failAt = nil
	if desktop.Active(r.data) != acctB {
		t.Fatal("not on B")
	}
	if v, _ := r.l.View(); v.CutOff || v.Leftover != 1 {
		t.Fatalf("view: %+v", v)
	}
	windows.CloseHandle(held)
	held = 0
	ok, err := r.l.Recover()
	if err != nil || ok {
		t.Fatalf("recover: %v %v", ok, err)
	}
	if desktop.Active(r.data) != acctB || !maps.Equal(itemsIn(t, r.data), itemFiles("B")) {
		t.Fatal("the switch was undone")
	}
	r.noStage()
}

// An app that names an account but holds no token (its own Log out leaves
// the name), yet has cookies and site storage: those are set aside too,
// never deleted with the switch, and never over the kept sign-in of the
// account it names.
func TestNamedWithoutTokenSetAside(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	keptA := tree(t, filepath.Join(r.l.Store, acctA))
	r.clearItems()
	for p, c := range itemFiles("C") {
		r.write(p, c)
	}
	r.writeCfg(appCfg(loginOf(acctA, "A")[2:], "\n")) // names A, no token
	left := itemsIn(t, r.data)
	res, err := r.l.Use(acctB)
	must(t, err)
	if res.Stashed == "" || res.Kept || res.From != acctA {
		t.Fatalf("use: %+v", res)
	}
	if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, res.Stashed)), left) {
		t.Fatal("what the app had is not set aside")
	}
	sameTree(t, "A as kept", keptA, tree(t, filepath.Join(r.l.Store, acctA)))
	if desktop.Active(r.data) != acctB || !maps.Equal(itemsIn(t, r.data), itemFiles("B")) {
		t.Fatal("not on B")
	}
}

// The store has permissions of its own: only the user and SYSTEM, not what
// the folder it is in passes on (BATON_HOME may be anywhere), and every
// kept sign-in inherits them. A store made before gets them as well.
func TestStoreHasItsOwnPermissions(t *testing.T) {
	r := newRig(t)
	must(t, os.MkdirAll(r.l.Store, 0o700)) // made before, inheriting
	_, err := r.l.Blank()
	must(t, err)
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	must(t, err)
	sd, err := windows.GetNamedSecurityInfo(r.l.Store, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	must(t, err)
	// D:P (protected: nothing inherited), maybe with AI (auto-inherited),
	// then the two entries and no other.
	got, aces := sd.String(), "(A;OICI;FA;;;"+u.User.Sid.String()+")(A;OICI;FA;;;SY)"
	_, dacl, _ := strings.Cut(got, "D:")
	if !strings.HasPrefix(dacl, "P") || !strings.HasSuffix(dacl, aces) || strings.Count(dacl, "(") != 2 {
		t.Fatalf("the store's permissions: %s, want D:P%s", got, aces)
	}
	for _, p := range []string{filepath.Join(r.l.Store, acctA), filepath.Join(r.l.Store, acctA, "Network", "Cookies")} {
		sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		must(t, err)
		dacl, _, err := sd.DACL()
		must(t, err)
		if dacl == nil || dacl.AceCount != 2 {
			t.Fatalf("%s: %s", p, sd.String())
		}
	}
}

// holdOpen opens p as another program may (an antivirus, the indexer):
// shared for reading and writing, not for delete, so it cannot be deleted
// or replaced while held. The handle is closed by the func, or at the end.
func holdOpen(t *testing.T, p string) func() {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	must(t, err)
	h, err := windows.CreateFile(u, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	must(t, err)
	closed := false
	release := func() {
		if !closed {
			closed = true
			windows.CloseHandle(h)
		}
	}
	t.Cleanup(release)
	return release
}

// cutBeforeFlip is a Use (to B) cut off once its journal said "config" but
// before config.json was written: the app's items are B's, its config.json
// is still A's, as the switch read it.
func (r *rig) cutBeforeFlip() {
	r.t.Helper()
	r.stopAt("flip", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		r.t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	s := stages(r.data, true)
	if len(s) != 1 {
		r.t.Fatal("setup: no stage")
	}
	j, err := readJournal(s[0])
	must(r.t, err)
	if j.Phase != phaseConfig || j.Flipped || desktop.Active(r.data) != acctA || r.read(filepath.Join("Network", "Cookies")) != "cookies-B" {
		r.t.Fatalf("setup: %+v, on %s", j, desktop.Active(r.data))
	}
}

// Cut off after the journal said "config" but before config.json was
// written, then the app ran on A's config.json and rewrote it (renewed A's
// tokens). The flip never landed, so Settle puts A's items back, as Recover
// does, instead of leaving A's tokens with B's cookies and site storage and
// deleting A's own with the stage. Uninstall, which settles and then deletes
// every kept sign-in, leaves A whole too.
func TestSettleWhenTheFlipNeverLanded(t *testing.T) {
	for _, how := range []string{"settle", "uninstall", "recover"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			r.cutBeforeFlip()
			fresh := loginOf(acctA, "A-fresh")
			r.writeCfg(r.cfg().splice(fresh))
			var ok bool
			var err error
			if how == "recover" {
				ok, err = r.l.Recover()
			} else {
				ok, err = r.l.Settle()
			}
			if err != nil || !ok {
				t.Fatalf("%s: %v %v", how, ok, err)
			}
			if how == "uninstall" {
				if n, err := r.l.turnOff(); err != nil || n != 2 {
					t.Fatalf("turn off: %d %v", n, err)
				}
			}
			if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
				t.Fatalf("A's items are not back: cookies %q", r.read(filepath.Join("Network", "Cookies")))
			}
			sameLogin(t, "A as the app renewed it", fresh, r.cfg().login())
			r.noStage()
			if v, _ := r.l.View(); v.CutOff || v.Leftover != 0 {
				t.Fatalf("view: %+v", v)
			}
		})
	}
}

// Cut off after the flip landed (the journal says so), then the app was
// signed in to A again inside it: Settle leaves the app as it is, A's new
// sign-in whole, and does not put A's old items back over it.
func TestSettleLeavesALandedFlipBackOnTheAccountItLeft(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	r.cutAfterFlip(acctB)
	if j, err := readJournal(stages(r.data, true)[0]); err != nil || !j.Flipped {
		t.Fatalf("setup: %+v %v", j, err)
	}
	r.signIn(acctA, "A-new")
	ok, err := r.l.Settle()
	if err != nil || !ok {
		t.Fatalf("settle: %v %v", ok, err)
	}
	if !maps.Equal(itemsIn(t, r.data), itemFiles("A-new")) {
		t.Fatalf("A's new items were replaced: cookies %q", r.read(filepath.Join("Network", "Cookies")))
	}
	sameLogin(t, "A's new sign-in", loginOf(acctA, "A-new"), r.cfg().login())
	r.noStage()
}

// A switch from a signed-out app cut off before its flip, then the user
// signed in inside the app, which has not named the account yet. That could
// be the app on the config.json the switch left or on the one it wrote:
// Settle leaves it as it is, and never deletes the new sign-in's cookies.
func TestSettleKeepsASignInNotNamedYetFromSignedOut(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	_, err := r.l.Blank()
	must(t, err)
	r.stopAt("flip", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	c := loginOf(acctC, "C")
	r.ranOn(append(c[:2:2], c[3:]...), "cookies-C") // no lastKnownAccountUuid
	ok, err := r.l.Settle()
	if err != nil || !ok {
		t.Fatalf("settle: %v %v", ok, err)
	}
	if got := r.read(filepath.Join("Network", "Cookies")); got != "cookies-C" {
		t.Fatalf("the new sign-in's cookies: %q", got)
	}
	if o := r.cfg(); len(o.login()) != 5 {
		t.Fatalf("the new sign-in's values: %d", len(o.login()))
	}
	r.noStage()
}

// The same cut-off switch from a signed-out app, then a sign-in the app has
// not named yet: Recover, Use and Blank change nothing and say so, as they do
// after any switch, instead of taking the app for one whose flip never landed
// and deleting the new sign-in's cookies with the items the switch moved in.
// Once the app names the account, Recover keeps that sign-in first.
func TestRecoverKeepsASignInNotNamedYetFromSignedOut(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	_, err := r.l.Blank()
	must(t, err)
	r.stopAt("flip", errCrash)
	if _, err := r.l.Use(acctB); err != errCrash {
		t.Fatalf("setup: %v", err)
	}
	r.l.failAt = nil
	c := loginOf(acctC, "C")
	r.ranOn(append(c[:2:2], c[3:]...), "cookies-C") // no lastKnownAccountUuid
	before, store := tree(t, r.data), tree(t, r.l.Store)
	wantDetail(t, "recover", recErr(r.l.Recover()), CodeUnsavedCurrent, "no_account")
	wantDetail(t, "use", useErr(r.l.Use(acctB)), CodeUnsavedCurrent, "no_account")
	wantDetail(t, "blank", useErr(r.l.Blank()), CodeUnsavedCurrent, "no_account")
	sameTree(t, "the app's folder", before, tree(t, r.data))
	sameTree(t, "the store", store, tree(t, r.l.Store))

	r.ranOn(c, "cookies-C")
	ok, err := r.l.Recover()
	if err != nil || !ok {
		t.Fatalf("recover once named: %v %v", ok, err)
	}
	sameLogin(t, "C kept", c, r.keptLogin(acctC))
	if b, _ := os.ReadFile(filepath.Join(r.l.Store, acctC, "Network", "Cookies")); string(b) != "cookies-C" {
		t.Fatalf("C's cookies kept: %q", b)
	}
	r.noStage()
}

// When the journal of a switch that Recover undid cannot be deleted
// (another program holds it open), the stage is marked done instead, as a
// switch that is done marks it: Recover reports it undone, the app is no
// longer shown as cut off, and the next switch does not undo it again over
// what the app has by then.
func TestRestoreWhoseJournalIsHeld(t *testing.T) {
	r := newRig(t)
	r.keptAll()
	was := moveWait
	moveWait = 300 * time.Millisecond
	t.Cleanup(func() { moveWait = was })
	r.cutAfterFlip(acctB)
	release := holdOpen(t, filepath.Join(stages(r.data, true)[0], "journal.json"))
	ok, err := r.l.Recover()
	if err != nil || !ok {
		t.Fatalf("recover: %v %v", ok, err)
	}
	if desktop.Active(r.data) != acctA || !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
		t.Fatal("A is not back")
	}
	if v, _ := r.l.View(); v.CutOff || v.Leftover != 1 {
		t.Fatalf("view: %+v", v)
	}
	release()
	// The user then changes the app's account to C inside it.
	r.signIn(acctC, "C")
	res, err := r.l.Use(acctB)
	if err != nil || res.Recovered || res.From != acctC || !res.Kept {
		t.Fatalf("use: %+v %v", res, err)
	}
	sameLogin(t, "C kept as C", loginOf(acctC, "C"), r.keptLogin(acctC))
	sameLogin(t, "A kept as before", loginOf(acctA, "A"), r.keptLogin(acctA))
	if b, _ := os.ReadFile(filepath.Join(r.l.Store, acctA, "Network", "Cookies")); string(b) != "cookies-A" {
		t.Fatalf("A's kept cookies: %q", b)
	}
	r.noStage()
}

// A put back that stopped on a held file once config.json was A's again
// (Recover, or the switch's own undo), and the app then rewrote config.json:
// the journal's note that the flip landed is old news, so Settle finishes
// putting A back, instead of leaving A's tokens with B's cookies and site
// storage and deleting A's own with the stage. Uninstall leaves A whole too.
func TestSettleAfterAPutBackThatStopped(t *testing.T) {
	for _, how := range []string{"ran on B, then put back", "put back, then ran on A", "the switch's own undo", "uninstall"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			was := moveWait
			moveWait = 300 * time.Millisecond
			t.Cleanup(func() { moveWait = was })
			cookies := filepath.Join(r.data, "Network", "Cookies")
			want := loginOf(acctA, "A-fresh")
			if how == "the switch's own undo" {
				var release func()
				r.l.failAt = func(s string) error {
					if s == "verify" {
						release = holdOpen(t, cookies)
						return errTest
					}
					return nil
				}
				wantCode(t, "use", useErr(r.l.Use(acctB)), CodeSwitchUnrestored)
				r.l.failAt = nil
				release()
			} else {
				r.cutAfterFlip(acctB)
				if how == "ran on B, then put back" {
					r.ranOn(loginOf(acctB, "B-fresh"), "cookies-B-fresh")
					want = loginOf(acctA, "A")
				}
				release := holdOpen(t, cookies)
				wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
				release()
			}
			if how != "ran on B, then put back" {
				r.writeCfg(r.cfg().splice(want)) // the app ran on A's config.json
			}
			ok, err := r.l.Settle()
			if err != nil || !ok {
				t.Fatalf("settle: %v %v", ok, err)
			}
			if how == "uninstall" {
				if _, err := r.l.turnOff(); err != nil {
					t.Fatal(err)
				}
			}
			if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
				t.Fatalf("A's items are not back: cookies %q", r.read(filepath.Join("Network", "Cookies")))
			}
			sameLogin(t, "A's six", want, r.cfg().login())
			r.noStage()
		})
	}
}

// Cut off while items moved, then the app was signed in to C inside it, over
// what of A had not moved out yet. Recover keeps C, and puts A back whole:
// what the stage lacks of A comes from A's kept copy (with no whole one it
// changes nothing). Settle and uninstall leave the app on C, A kept as it
// was. From a signed-out app, a sign-in not named yet is never thrown away.
func TestCutWhileMovingThenSignedInElsewhere(t *testing.T) {
	for _, how := range []string{"recover", "kept copy damaged", "settle", "uninstall", "from signed out"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			if how == "from signed out" {
				_, err := r.l.Blank()
				must(t, err)
			}
			r.stopAt("move "+items[2].name, errCrash)
			if _, err := r.l.Use(acctB); err != errCrash {
				t.Fatalf("setup: %v", err)
			}
			r.l.failAt = nil
			r.signIn(acctC, "C")
			c := loginOf(acctC, "C")
			if how == "from signed out" {
				c = append(c[:2:2], c[3:]...) // not named yet
				r.writeCfg(appCfg(c, "\n"))
			}
			if how == "kept copy damaged" {
				must(t, os.Remove(filepath.Join(r.l.Store, acctA, "meta.json")))
			}
			before, store := tree(t, r.data), tree(t, r.l.Store)
			switch how {
			case "from signed out":
				wantDetail(t, "recover", recErr(r.l.Recover()), CodeUnsavedCurrent, "no_account")
				sameTree(t, "the app's folder", before, tree(t, r.data))
				sameTree(t, "the store", store, tree(t, r.l.Store))
			case "kept copy damaged":
				wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
				sameTree(t, "the app's folder", before, tree(t, r.data))
				sameTree(t, "the store", store, tree(t, r.l.Store))
			case "recover":
				ok, err := r.l.Recover()
				if err != nil || !ok {
					t.Fatalf("recover: %v %v", ok, err)
				}
				if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
					t.Fatalf("A is not back whole: cookies %q", r.read(filepath.Join("Network", "Cookies")))
				}
				sameLogin(t, "A's six", loginOf(acctA, "A"), r.cfg().login())
				sameLogin(t, "C kept", loginOf(acctC, "C"), r.keptLogin(acctC))
				if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, acctC)), itemFiles("C")) {
					t.Fatal("C's kept items")
				}
				r.noStage()
				return
			}
			keptA := tree(t, filepath.Join(r.l.Store, acctA))
			ok, err := r.l.Settle()
			if err != nil || !ok {
				t.Fatalf("settle: %v %v", ok, err)
			}
			if how == "uninstall" {
				if _, err := r.l.turnOff(); err != nil {
					t.Fatal(err)
				}
			}
			if !maps.Equal(itemsIn(t, r.data), itemFiles("C")) {
				t.Fatalf("C is not left as it is: cookies %q", r.read(filepath.Join("Network", "Cookies")))
			}
			sameLogin(t, "C's six", c, r.cfg().login())
			r.noStage()
			if how == "settle" {
				sameTree(t, "A as kept", keptA, tree(t, filepath.Join(r.l.Store, acctA)))
			}
		})
	}
}

// The app's own Log out leaves it naming A with no token, and with cookies
// and site storage of its own (set aside in the store before a switch). A
// switch from there left no sign-in, so once it is cut off and the app is
// signed in to A again, that new sign-in is never taken for one the switch
// left: Recover keeps it first and puts the signed-out app back, Settle and
// uninstall leave it as it is, and the next switch keeps it whole.
func TestSignedInAgainAfterItsOwnLogOut(t *testing.T) {
	ls, cookies := filepath.Join("Local Storage", "leveldb", "000003.log"), filepath.Join("Network", "Cookies")
	for _, cut := range []string{"config", "verify"} {
		for _, how := range []string{"recover", "settle", "uninstall", "next use"} {
			t.Run(cut+", "+how, func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				r.clearItems()
				r.write(cookies, "cookies-out")
				r.write(ls, "ls-out")
				out := loginOf(acctA, "A")
				out[0].raw, out[1].raw = []byte(`""`), []byte(`""`)
				r.writeCfg(appCfg(out, "\n"))
				r.stopAt(cut, errCrash)
				if _, err := r.l.Use(acctB); err != errCrash {
					t.Fatalf("setup: %v", err)
				}
				r.l.failAt = nil
				r.signIn(acctA, "A-new")
				keptNew := func() {
					t.Helper()
					sameLogin(t, "A kept", loginOf(acctA, "A-new"), r.keptLogin(acctA))
					if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, acctA)), itemFiles("A-new")) {
						t.Fatal("A's kept items are not its new sign-in's")
					}
				}
				switch how {
				case "recover":
					ok, err := r.l.Recover()
					if err != nil || !ok {
						t.Fatalf("recover: %v %v", ok, err)
					}
					keptNew()
					sameLogin(t, "the signed-out app's six", out, r.cfg().login())
					if got := itemsIn(t, r.data); !maps.Equal(got, map[string]string{ls: "ls-out", cookies: "cookies-out"}) {
						t.Fatalf("the signed-out app's items: %v", got)
					}
				case "settle", "uninstall":
					ok, err := r.l.Settle()
					if err != nil || !ok {
						t.Fatalf("settle: %v %v", ok, err)
					}
					if how == "uninstall" {
						if _, err := r.l.turnOff(); err != nil {
							t.Fatal(err)
						}
					}
					if !maps.Equal(itemsIn(t, r.data), itemFiles("A-new")) {
						t.Fatalf("A's new sign-in is not left as it is: cookies %q", r.read(cookies))
					}
					sameLogin(t, "A's new sign-in", loginOf(acctA, "A-new"), r.cfg().login())
				case "next use":
					res, err := r.l.Use(acctB)
					if err != nil || !res.Recovered || desktop.Active(r.data) != acctB {
						t.Fatalf("use: %+v %v", res, err)
					}
					keptNew()
				}
				r.noStage()
			})
		}
	}
}

// A switch from A cut off before its flip, or put back part way after it
// (config.json is A's again), then an update of the app rewrote config.json,
// still A's, in a shape baton does not know. That is the config.json of the
// sign-in the switch left: Recover and Settle put A's items back under it
// and leave it as the app wrote it, instead of leaving A's tokens with B's
// cookies and site storage and deleting A's own with the stage.
func TestUnknownShapeOnTheConfigOfTheAccountLeft(t *testing.T) {
	for _, cut := range []string{"before the flip", "put back part way"} {
		for _, how := range []string{"recover", "settle", "uninstall"} {
			t.Run(cut+", "+how, func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				was := moveWait
				moveWait = 300 * time.Millisecond
				t.Cleanup(func() { moveWait = was })
				if cut == "before the flip" {
					r.cutBeforeFlip()
				} else {
					r.cutAfterFlip(acctB)
					release := holdOpen(t, filepath.Join(r.data, "Network", "Cookies"))
					wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
					release()
				}
				r.writeCfg(r.cfg().splice(append(loginOf(acctA, "A-fresh"), kv{"oauth:tokenCacheV3", []byte(`"x"`)})))
				want := r.read("config.json")
				var ok bool
				var err error
				if how == "recover" {
					ok, err = r.l.Recover()
				} else {
					ok, err = r.l.Settle()
				}
				if err != nil || !ok {
					t.Fatalf("%s: %v %v", how, ok, err)
				}
				if how == "uninstall" {
					if _, err := r.l.turnOff(); err != nil {
						t.Fatal(err)
					}
				}
				if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
					t.Fatalf("A's items are not back: cookies %q", r.read(filepath.Join("Network", "Cookies")))
				}
				if r.read("config.json") != want {
					t.Fatal("config.json is not as the app wrote it")
				}
				r.noStage()
			})
		}
	}
}

// Put it back while another program holds config.json open: the write
// fails, config.json is still as the switch wrote it, and the note that a
// restore began goes again, so nothing has changed. The flip is still known
// to be in place: once the app is signed in to A again, Recover and Settle
// leave that new sign-in whole.
func TestPutBackThatCouldNotWriteConfig(t *testing.T) {
	for _, how := range []string{"recover", "settle"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			was := moveWait
			moveWait = 300 * time.Millisecond
			t.Cleanup(func() { moveWait = was })
			r.cutAfterFlip(acctB)
			before := tree(t, r.data)
			release := holdOpen(t, filepath.Join(r.data, "config.json"))
			wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
			release()
			sameTree(t, "the app's folder", before, tree(t, r.data))
			r.signIn(acctA, "A-new")
			var ok bool
			var err error
			if how == "recover" {
				ok, err = r.l.Recover()
			} else {
				ok, err = r.l.Settle()
			}
			if err != nil || !ok {
				t.Fatalf("%s: %v %v", how, ok, err)
			}
			if !maps.Equal(itemsIn(t, r.data), itemFiles("A-new")) {
				t.Fatalf("A's new items were replaced: cookies %q", r.read(filepath.Join("Network", "Cookies")))
			}
			sameLogin(t, "A's new sign-in", loginOf(acctA, "A-new"), r.cfg().login())
			r.noStage()
		})
	}
}

// A switch cut off, then the app's folders became a layout baton does not
// know: a second Claude-Data\claude_* folder holds a config.json (after an
// update of the app, say), or the one lost its config.json. The switch is
// still seen as cut off, by View and by CutOffNow (which uninstall asks
// before it deletes the kept copies), and Recover and Settle work in the
// folder it was cut off in: uninstall settles it, as it says, instead of
// leaving what it set aside behind with nothing left to settle it.
func TestCutOffAfterTheLayoutChanged(t *testing.T) {
	for _, how := range []string{"second folder", "config gone"} {
		for _, action := range []string{"recover", "uninstall"} {
			t.Run(how+", "+action, func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				// The app's folders as baton reads them for real.
				t.Setenv("BATON_DESKTOP_DATA", "")
				t.Setenv("LOCALAPPDATA", r.root)
				r.l.dataFn = func() string { return r.data }
				if _, err := r.l.dataDir(); err != nil {
					t.Fatalf("setup: %v", err)
				}
				switch how {
				case "second folder":
					r.cutAfterFlip(acctB)
					other := filepath.Join(r.root, "Claude-Data", "claude_other")
					must(t, os.MkdirAll(other, 0o700))
					must(t, os.WriteFile(filepath.Join(other, "config.json"), appCfg(nil, "\n"), 0o600))
				case "config gone":
					r.stopAt("config", errCrash)
					if _, err := r.l.Use(acctB); err != errCrash {
						t.Fatalf("setup: %v", err)
					}
					r.l.failAt = nil
					must(t, os.Remove(filepath.Join(r.data, "config.json")))
				}
				if _, err := r.l.dataDir(); Code(err) != CodeUnsupportedLayout {
					t.Fatalf("setup: the layout is still one baton knows: %v", err)
				}
				if v, _ := r.l.View(); !v.CutOff {
					t.Fatalf("view: %+v", v)
				}
				if cut, err := r.l.CutOffNow(); !cut || err != nil {
					t.Fatalf("cut off now: %v %v", cut, err)
				}
				if action == "recover" {
					ok, err := r.l.Recover()
					if err != nil || !ok {
						t.Fatalf("recover: %v %v", ok, err)
					}
					if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
						t.Fatalf("A's items are not back: cookies %q", r.read(filepath.Join("Network", "Cookies")))
					}
					if how == "second folder" {
						sameLogin(t, "A's six", loginOf(acctA, "A"), r.cfg().login())
					}
				} else {
					// As uninstall does it.
					if ok, err := r.l.Settle(); err != nil || !ok {
						t.Fatalf("settle: %v %v", ok, err)
					}
					if _, err := r.l.turnOff(); err != nil {
						t.Fatal(err)
					}
					want, on := itemFiles("A"), acctA
					if how == "second folder" {
						want, on = itemFiles("B"), acctB // the flip landed: left as it is
					}
					if !maps.Equal(itemsIn(t, r.data), want) {
						t.Fatalf("the app's items: cookies %q", r.read(filepath.Join("Network", "Cookies")))
					}
					if how == "second folder" && desktop.Active(r.data) != on {
						t.Fatalf("not on %s", on)
					}
				}
				r.noStage()
				if cut, err := r.l.CutOffNow(); cut || err != nil {
					t.Fatalf("cut off now, after: %v %v", cut, err)
				}
				if v, _ := r.l.View(); v.CutOff {
					t.Fatalf("view after: %+v", v)
				}
			})
		}
	}
}

// A switch from A cut off once it wrote config.json (a Use, or a Blank),
// then the app was signed in to A again and signed out by its own Log out:
// it names A with no token, and every item of A's older sign-in is still
// set aside. With A's kept copy damaged, or switching off, the stage holds
// the only copy of that sign-in: Recover puts it back whole, its six values
// with its items, instead of leaving the app signed out and deleting A's
// tokens with the stage.
func TestSignedInAgainThenLoggedOutPutsBackWhole(t *testing.T) {
	ls, cookies := filepath.Join("Local Storage", "leveldb", "000003.log"), filepath.Join("Network", "Cookies")
	for _, to := range []string{acctB, ""} {
		for _, keep := range []string{"kept copy damaged", "off"} {
			t.Run(fmt.Sprintf("to %q, %s", to, keep), func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				r.cutAfterFlip(to)
				r.signIn(acctA, "A-new")
				out := loginOf(acctA, "A-new")
				out[0].raw, out[1].raw = []byte(`""`), []byte(`""`)
				r.writeCfg(r.cfg().splice(out))
				r.clearItems()
				r.write(cookies, "cookies-out")
				r.write(ls, "ls-out")
				if keep == "off" {
					if _, err := r.l.turnOff(); err != nil {
						t.Fatal(err)
					}
				} else {
					must(t, os.Remove(filepath.Join(r.l.Store, acctA, "meta.json")))
				}
				ok, err := r.l.Recover()
				if err != nil || !ok {
					t.Fatalf("recover: %v %v", ok, err)
				}
				if !maps.Equal(itemsIn(t, r.data), itemFiles("A")) {
					t.Fatalf("A's items are not back: cookies %q", r.read(cookies))
				}
				sameLogin(t, "A's six", loginOf(acctA, "A"), r.cfg().login())
				r.noStage()
			})
		}
	}
}

// noIDB is A's sign-in as itemFiles has it, but with no IndexedDB.
func noIDB(files map[string]string) map[string]string {
	out := map[string]string{}
	for p, c := range files {
		if !strings.HasPrefix(p, "IndexedDB"+string(filepath.Separator)) {
			out[p] = c
		}
	}
	return out
}

// A's sign-in has no IndexedDB, B's has one, so the switch moves B's in
// where A had none. Put back part way (Recover, held at Local Storage, the
// last it puts back) after it took B's IndexedDB out again, with config.json
// A's again; then the app ran on A, made an IndexedDB of its own and renewed
// A's tokens and cookies. Recover, Settle and the next switch keep what the
// app made, instead of taking it for the copy the switch moved in.
func TestPutBackThatStoppedThenTheAppMadeItsOwn(t *testing.T) {
	idb := filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log")
	cookies := filepath.Join("Network", "Cookies")
	for _, how := range []string{"recover", "settle", "next use"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			was := moveWait
			moveWait = 300 * time.Millisecond
			t.Cleanup(func() { moveWait = was })
			must(t, os.RemoveAll(filepath.Join(r.data, "IndexedDB")))
			r.cutAfterFlip(acctB)
			j, err := readJournal(stages(r.data, true)[0])
			must(t, err)
			if it := j.Items[2]; it.Name != "IndexedDB" || it.HadOld || !it.HasNew {
				t.Fatalf("setup: %+v", it)
			}
			release := holdOpen(t, filepath.Join(r.data, "Local Storage", "leveldb", "000003.log"))
			wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
			release()
			if desktop.Active(r.data) != acctA || exists(filepath.Join(r.data, "IndexedDB")) {
				t.Fatal("setup: the put back did not stop after it took B's IndexedDB out")
			}
			r.write(idb, "idb-A-since")
			fresh := loginOf(acctA, "A-fresh")
			r.ranOn(fresh, "cookies-A-fresh")
			want := noIDB(itemFiles("A"))
			want[idb], want[cookies] = "idb-A-since", "cookies-A-fresh"
			switch how {
			case "recover", "settle":
				var ok bool
				if how == "recover" {
					ok, err = r.l.Recover()
				} else {
					ok, err = r.l.Settle()
				}
				if err != nil || !ok {
					t.Fatalf("%s: %v %v", how, ok, err)
				}
				if got := itemsIn(t, r.data); !maps.Equal(got, want) {
					t.Fatalf("A's items: %v", got)
				}
				sameLogin(t, "A as the app renewed it", fresh, r.cfg().login())
			case "next use":
				res, err := r.l.Use(acctB)
				if err != nil || !res.Recovered || !res.Kept || desktop.Active(r.data) != acctB {
					t.Fatalf("use: %+v %v", res, err)
				}
				sameLogin(t, "A kept as the app renewed it", fresh, r.keptLogin(acctA))
				if got := itemsIn(t, filepath.Join(r.l.Store, acctA)); !maps.Equal(got, want) {
					t.Fatalf("A's kept items: %v", got)
				}
			}
			r.noStage()
		})
	}
}

// A's sign-in has no IndexedDB. A switch from it cut off after the flip
// (a Use put back part way after it took B's IndexedDB out, or a Blank,
// which moved none in), then the app made one off A's sign-in: signed in to
// C, or still signed out after the Blank. Recover puts A back exactly as it
// was, with no IndexedDB of another's under A's tokens, and keeps C whole;
// also when a Recover that kept C first stopped (held at the cookies)
// before it took C's IndexedDB out, once config.json named A again.
func TestPutBackThatStoppedThenSignedInElsewhere(t *testing.T) {
	idb := filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log")
	for _, how := range []string{"use, put back part way", "blank", "blank, ran signed out", "blank, C kept, put back part way"} {
		t.Run(how, func(t *testing.T) {
			r := newRig(t)
			r.keptAll()
			was := moveWait
			moveWait = 300 * time.Millisecond
			t.Cleanup(func() { moveWait = was })
			must(t, os.RemoveAll(filepath.Join(r.data, "IndexedDB")))
			if how == "use, put back part way" {
				r.cutAfterFlip(acctB)
				release := holdOpen(t, filepath.Join(r.data, "Local Storage", "leveldb", "000003.log"))
				wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
				release()
			} else {
				r.cutAfterFlip("")
			}
			if how == "blank, ran signed out" {
				r.write(filepath.Join("Network", "Cookies"), "cookies-out")
				r.write(idb, "idb-out")
			} else {
				r.signIn(acctC, "C")
			}
			if how == "blank, C kept, put back part way" {
				release := holdOpen(t, filepath.Join(r.data, "Network", "Cookies"))
				wantCode(t, "recover", recErr(r.l.Recover()), CodeSwitchUnrestored)
				release()
				if desktop.Active(r.data) != acctA || r.read(idb) != "idb-C" {
					t.Fatal("setup: the put back did not stop before C's IndexedDB, with config.json A's")
				}
			}
			ok, err := r.l.Recover()
			if err != nil || !ok {
				t.Fatalf("recover: %v %v", ok, err)
			}
			if got := itemsIn(t, r.data); !maps.Equal(got, noIDB(itemFiles("A"))) {
				t.Fatalf("A's items: %v", got)
			}
			sameLogin(t, "A's six", loginOf(acctA, "A"), r.cfg().login())
			if how != "blank, ran signed out" {
				sameLogin(t, "C kept", loginOf(acctC, "C"), r.keptLogin(acctC))
				if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, acctC)), itemFiles("C")) {
					t.Fatal("C's kept items are not whole")
				}
			}
			r.noStage()
		})
	}
}

// Cut off after the flip landed (a Use, or a Blank), then the app was signed
// in to A again inside it: Recover and the next switch leave that new
// sign-in whole, as Settle does, instead of putting A's older items under
// its tokens; the next switch keeps it as A's.
func TestRecoverLeavesALandedFlipBackOnTheAccountItLeft(t *testing.T) {
	for _, to := range []string{acctB, ""} {
		for _, how := range []string{"recover", "next use"} {
			t.Run(fmt.Sprintf("to %q, %s", to, how), func(t *testing.T) {
				r := newRig(t)
				r.keptAll()
				r.cutAfterFlip(to)
				r.signIn(acctA, "A-new")
				if how == "recover" {
					ok, err := r.l.Recover()
					if err != nil || !ok {
						t.Fatalf("recover: %v %v", ok, err)
					}
					if !maps.Equal(itemsIn(t, r.data), itemFiles("A-new")) {
						t.Fatalf("A's new items were replaced: cookies %q", r.read(filepath.Join("Network", "Cookies")))
					}
					sameLogin(t, "A's new sign-in", loginOf(acctA, "A-new"), r.cfg().login())
					r.noStage()
					return
				}
				res, err := r.l.Use(acctB)
				if err != nil || !res.Recovered || !res.Kept {
					t.Fatalf("use: %+v %v", res, err)
				}
				sameLogin(t, "A kept as the app had it", loginOf(acctA, "A-new"), r.keptLogin(acctA))
				if !maps.Equal(itemsIn(t, filepath.Join(r.l.Store, acctA)), itemFiles("A-new")) {
					t.Fatal("A's kept items are not its new sign-in's")
				}
				r.noStage()
			})
		}
	}
}
