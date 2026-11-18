package desklogin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"baton/internal/app"
	"baton/internal/desktop"

	"golang.org/x/sys/windows"
)

// The state matrix: every place baton knows a switch can be cut off, what
// the app may have been through since, and each thing the user can do then,
// all held to the same four rules.
//
//	I1  Never a half-switched app: once no switch is pending, the app's
//	    tokens, the account it names and its items are one sign-in's, or it
//	    holds no token at all. A kept copy is one sign-in's too. A renewal
//	    (the app ran on it) renews its tokens and its cookies together, so
//	    a renewal's tokens over the older cookies are two sign-ins mixed.
//	I2  A sign-in that is whole anywhere before an action (in the app, kept,
//	    or in what a pending switch set aside, with what of it is still in
//	    the app) is whole somewhere after it, with every file it had, each
//	    as it was or renewed since, unless the user asked for it to go. A
//	    renewal is a sign-in of its own: neither the older copy nor an older
//	    file stands in for it, while it stands in for the older one.
//	    Settle ("Leave it as it is") leaves a whole sign-in in the app where
//	    it is, file for file, and after uninstall (Settle, then TurnOff,
//	    which deletes every kept copy) the app still has it.
//	I3  Nothing stays stuck: Settle ends every stage, in every state, once
//	    no other program holds a file it reads.
//	I4  An action that refuses changes nothing, byte for byte.
//
// Dimensions (every combination that can happen):
//
//	kind   Use(B) from A; Use(B) from an app signed out by a Blank; Blank
//	       from A; Use(B) and Blank from an app its own Log out left (it
//	       names A, holds no token, and has cookies and site storage of its
//	       own, in only some of the items); and Use(B) and Blank from A whose
//	       sign-in has no IndexedDB (Ai), which B's has: an item the sign-in
//	       the switch left did not have
//	cut    moving-some (two items moved), moving-all (every item moved,
//	       config.json not yet looked at), before-flip (journal "config",
//	       config.json not written), flip-unnoted (written, the note held
//	       off by a hold on the journal), flipped, commit (all done but the
//	       journal), rollback-failed (the switch's own undo stopped on a
//	       held file after it wrote config.json back)
//	undo   none; a Recover that stopped on a held file before the app was
//	       used (late: all but Local Storage put back); one that stopped
//	       after it was used (early: held at the cookies, as rollback-failed
//	       is); one killed between its note that it writes config.json back
//	       and the write (flipped)
//	since  nothing; ran on what it had (a setting changed, tokens and
//	       cookies renewed, and it makes the site storage it lacks, its
//	       sign-in's or, signed out, its own); signed out in the app; signed
//	       in to From again; signed in to From again, then signed out by its
//	       own Log out; signed in to a third account C, named; and not named
//	       yet; ran, and an update rewrote config.json in a shape baton does
//	       not know; signed in to C in that shape
//	keep   switching on with From's kept copy whole; on with it damaged;
//	       switching off (nothing can be kept)
//	action Recover; Settle, then TurnOff (uninstall); Use(A), Use(B),
//	       Use(C); Blank
//	held   nothing; or, while the action runs, another program holds open
//	       without sharing it for reading (an open with share mode 0) one of
//	       the files restore and Settle go by: config.json, the journal, or
//	       the config.json the switch read (stage\old), with Recover or
//	       Settle (with each action, in full: mHeldCells). A file that cannot
//	       be read says nothing of what it holds: the action refuses and
//	       changes nothing (Settle too: "try again"), or does what it does
//	       unheld.
//
// A Recover that stops on a held config.json must change nothing at all
// (the note it made goes again). That is checked once for each cut with a
// config.json to write back, and its states are then those of undo "none".
// Two more stops are tested on their own: a switch that cannot take baton's
// desktop lock while another program holds its journal, which must not
// leave a switch cut off (TestBusyWithItsJournalHeld), and a layout that
// changed after the cut (TestCutOffAfterTheLayoutChanged), which needs the
// environment, and a parallel test cannot change it.
//
// Where the right answer is not plain, the one that never loses a sign-in
// was chosen:
//
//	the app names From, with a token, the    Recover, the next switch and
//	switch left a sign-in of From's (the     Settle put From's items back
//	config.json it read held a token), and   under the app's tokens. The
//	the flip is not known to be in place     app may have run on a
//	(moving, not noted, or a restore began,  half-switched app; left as it
//	or was killed before it wrote): signed   is, that would mix two
//	in to From again, or ran on the half-    accounts. Put back, it makes at
//	switched app; baton cannot tell them     worst two sign-ins of From's
//	apart                                    own account one, still signed
//	                                         in, which the next switch keeps
//	                                         as From's (mixOK below). Only
//	                                         From's account: the app, or the
//	                                         kept copy of From, with an item
//	                                         of another account in it is
//	                                         half switched all the same
//	                                         (mixOneAccount); and no file of
//	                                         From's own sign-in gone, also
//	                                         one the app made since. Of
//	                                         another sign-in's (signed in to
//	                                         From again), an item From's set
//	                                         lacks goes with the rest of it:
//	                                         baton cannot tell it from the
//	                                         copy the switch moved in. It may
//	                                         look whole: ran while moving,
//	                                         the app's renewed tokens and
//	                                         cookies get From's older site
//	                                         storage back over what it made
//	                                         (mixedInto).
//	a file restore or Settle reads is held   the action refuses (Settle with
//	by another program that does not share   dl_switch_unrestored, "try
//	it for reading                           again") and changes nothing:
//	                                         an unreadable file is no
//	                                         answer. Not there at all is
//	                                         one (before the flip, a gone
//	                                         config.json plays no part).
//	the switch started from an app with no   a token there now is a sign-in
//	token (its own Log out left it naming    made since: Recover keeps it
//	From), then the app was signed in to     first and puts the signed-out
//	From again                               app back; Settle leaves it as
//	                                         it is. What of the signed-out
//	                                         app was never moved out stays
//	                                         under the restored app (no
//	                                         token: not a sign-in).
//	the flip landed (noted), then the app    Recover, the next switch and
//	was signed in to From again              Settle leave it as it is; the
//	                                         copy set aside is kept only
//	                                         when the kept one is not whole
//	                                         (superseded below, while off).
//	part of From was still in the app when   Recover keeps that sign-in,
//	it was signed in to another account      fills the rest of From from its
//	(cut while moving, or after a restore    kept copy, and puts From back;
//	began)                                   with no whole kept copy it
//	                                         changes nothing (Settle leaves
//	                                         the app as it is).
//	config.json in a shape baton does not    it names From (whose sign-in
//	know: it cannot be read for a token,     the switch left) and the flip
//	nor spliced                              is not known to be in place:
//	                                         Recover, Settle and the next
//	                                         switch put the items back and
//	                                         leave config.json as the app
//	                                         wrote it. Else Recover and the
//	                                         next switch change nothing, and
//	                                         Settle leaves it as it is.
//	Settle, switching off                    what the switch set aside goes,
//	                                         as the window says before it
//	                                         asks (nothing can be kept).
//	the app's own Log out since              the sign-in it ended is not
//	                                         counted as lost: Claude's
//	                                         servers ended it, so no copy of
//	                                         it signs anyone in any more.
//	the switch left a sign-in of From's;     Recover, the next switch and
//	then the app was signed in to From       Settle (unless the flip is
//	again and signed out by its own Log      noted as landed) put From's six
//	out (it names From, no token), with      values back with its items:
//	every item of From's still set aside     From whole again. If that Log
//	                                         out ended From's older sign-in
//	                                         (the app was on it), the app
//	                                         asks for a sign-in again;
//	                                         baton cannot tell the two
//	                                         apart, and left signed out,
//	                                         the older sign-in's tokens
//	                                         would go with the stage.
//	an item From's sign-in did not have      taken out into the stage, never
//	(Ai: no IndexedDB) that the switch       deleted at once: a put back that
//	moved in, or that the app made off       stopped, then the app run on
//	From's sign-in (another, kept first;     From, leaves what the app made
//	another account, or none, or no          there since as From's own. One
//	token)                                   found off From's sign-in by a
//	                                         put back that stopped before it
//	                                         took it out is noted, so the
//	                                         next try takes it out although
//	                                         config.json names From again.
//	                                         Made on From before the flip
//	                                         (moving), it is From's and stays.
//	Recover, switching off, the app unused   the copy of B the switch moved
//	since the switch                         in goes: TurnOff deleted B's
//	                                         kept copy, as the user asked,
//	                                         and the switch is put back.
//
// The states are built once each, through the package's own switch, its
// stops and holds on files, and copied (times and all) for every action. A
// sign-in here has one file in each item (mFiles), not the ten of
// itemFiles: items move whole, and every file baton copies goes through
// desktop.Guard, which takes milliseconds and hardly runs in parallel. So
// each state gets Recover, Settle and uninstall, one state in mEvery one of
// the next switches, each in turn, and one Recover or Settle again with a
// file held, going round (mHeldCells); the states a cut at "commit" leaves
// are skipped: byte for byte those of "flipped" but for the stage's name
// (mCut checks its journal); and the noidb kinds skip the states their item
// plays no part in (C not named yet, C in a shape baton does not know, A's
// kept copy damaged). That is BATON_MATRIX=all, some 2300 states and more
// than ten minutes. By default (go test ./... must fit in its ten minutes)
// about two states in five are checked, chosen so that every undo, since
// and keep that goes together is still checked in every kind and at every
// cut (mSampled), but for a few with undo "after": a pick that turns out not
// to stop part way is skipped, and no other is picked in its place.
// BATON_MATRIX=full runs every state, and every next switch on every state,
// each with every file held.

var mKinds = []struct {
	name     string
	start    string // the app before the switch: onA, out (a Blank's), logout (its own Log out's), onAi (A, no IndexedDB)
	from, to string
}{
	{"useB", "onA", acctA, acctB},
	{"useB-out", "out", "", acctB},
	{"blank", "onA", acctA, ""},
	{"useB-logout", "logout", acctA, acctB},
	{"blank-logout", "logout", acctA, ""},
	{"useB-noidb", "onAi", acctA, acctB},
	{"blank-noidb", "onAi", acctA, ""},
}

var mCuts = []string{"moving-some", "moving-all", "before-flip", "flip-unnoted", "flipped", "commit", "rollback-failed"}

var mUndo = []string{"none", "before", "after", "crash"}

var mSince = []string{"nothing", "ran", "signed-out", "from-again", "from-again-out", "c-named", "c-unnamed", "unknown", "c-unknown"}

var mKeep = []string{"on", "damaged", "off"}

// mSwitches are the next switches a state can get.
var mSwitches = []string{"use A", "use B", "use C", "blank"}

// mHeld are the files restore and Settle go by, which another program may
// hold open without sharing them for reading while an action runs.
var mHeld = []string{"config.json", "journal.json", `old\config.json`}

// mHeldCell is an action run while a file of mHeld is held.
type mHeldCell struct{ action, file string }

// mHeldCells are the actions of a state (acts) that run again with a file
// held, each in turn. In full, each action with each file. By default one:
// Recover or Settle, the two that read those files first (a next switch
// undoes through Recover's restore before anything else), with one file,
// going round the six with the state: at is since + 2*keep + undo, so over
// the states of a cut each of the six meets every since, every keep and
// every undo. A held refusal costs about what a Recover that undoes costs
// (the locks, and a look at every file after): every file with every action
// on every state would take some ten minutes more.
func mHeldCells(full bool, acts []string, at int) []mHeldCell {
	var out []mHeldCell
	if full {
		for _, a := range acts {
			for _, f := range mHeld {
				out = append(out, mHeldCell{a, f})
			}
		}
		return out
	}
	return append(out, mHeldCell{[]string{"recover", "settle"}[at%2], mHeld[at/2%len(mHeld)]})
}

// mKindHas says whether kind ki has states with since and keep at all:
// from-again needs a From, and A's kept copy damaged needs one to keep.
// Outside full, the noidb kinds also skip what their item plays no part in:
// a refusal that changes nothing, or a config.json no restore splices (C
// not named yet, C in a shape baton does not know), and A's kept copy
// damaged rather than whole.
func mKindHas(ki int, since, keep string, full bool) bool {
	k := mKinds[ki]
	switch {
	case (since == "from-again" || since == "from-again-out") && k.from == "",
		keep == "damaged" && k.from == "",
		!full && k.start == "onAi" && (since == "c-unnamed" || since == "c-unknown" || keep == "damaged"):
		return false
	}
	return true
}

// mCutsWith are the cuts (by index, "commit" left out as by default) that
// have states with undo: a Recover that stopped part way is made only where
// the switch did not undo itself, and one killed half way only after a flip.
func mCutsWith(undo string) []int {
	var out []int
	for ci, cut := range mCuts {
		switch {
		case cut == "commit",
			(undo == "before" || undo == "after") && cut == "rollback-failed",
			undo == "crash" && cut != "flipped":
			continue
		}
		out = append(out, ci)
	}
	return out
}

// mSampled says whether the state of kind ki, cut ci, undo ui, since si and
// keep kpi is one of those checked by default. For each undo, since and keep
// that go together, one cut in each kind and one kind at each cut, going
// round with the three: so each is checked in every kind and at every cut
// that has it, in about two states in five.
func mSampled(ki, ci, ui, si, kpi int) bool {
	n := ui*len(mSince)*len(mKeep) + si*len(mKeep) + kpi
	var kinds []int
	for k := range mKinds {
		if mKindHas(k, mSince[si], mKeep[kpi], false) {
			kinds = append(kinds, k)
		}
	}
	cuts := mCutsWith(mUndo[ui])
	return len(cuts) > 0 && cuts[(n+ki)%len(cuts)] == ci || len(kinds) > 0 && kinds[(n+ci)%len(kinds)] == ki
}

// mEvery: one state in mEvery gets a next switch, each of mSwitches in
// turn (each on about one state in 32: some 70 states with BATON_MATRIX=all,
// some 27 by default). A next
// switch costs about twice what Recover, Settle and uninstall cost
// together: it puts back as Recover does (checked on every state), then
// keeps and copies whole login sets.
const mEvery = 8

func TestStateMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("the state matrix takes minutes; run it without -short")
	}
	mode := os.Getenv("BATON_MATRIX") // "", "all" or "full"
	full, all := mode == "full", mode == "all" || mode == "full"
	// Each rig names its own folder (dataFn); this only says it is a test's.
	t.Setenv("BATON_DESKTOP_DATA", "matrix")
	t.Setenv("BATON_DESKTOP_RUNNING", "0")
	for _, k := range []string{"CLAUDECODE", "OneDrive", "OneDriveConsumer", "OneDriveCommercial"} {
		t.Setenv(k, "")
	}
	was := moveWait
	moveWait = 300 * time.Millisecond
	t.Cleanup(func() { moveWait = was })
	top, err := os.MkdirTemp("", "dm")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(top) })
	var mu sync.Mutex
	outcomes := map[string]int{}
	states, cells := 0, 0
	t.Cleanup(func() {
		var keys []string
		for k := range outcomes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "\n  %-56s %d", k, outcomes[k])
		}
		t.Logf("%d states, %d cells:%s", states, cells, b.String())
	})
	note := func(action, outcome string) {
		mu.Lock()
		outcomes[action+": "+outcome]++
		cells++
		mu.Unlock()
	}

	// The four starting points: the app on A, with A and B kept; that app
	// signed out by a Blank; that app signed out by its own Log out; and
	// that app signed in to A again, with no IndexedDB (B's has one).
	starts := map[string]string{"onA": filepath.Join(top, "onA"), "out": filepath.Join(top, "out"), "logout": filepath.Join(top, "logout"),
		"onAi": filepath.Join(top, "onAi")}
	r := openRig(t, starts["onA"])
	r.setOn(true)
	mSignIn(r, acctA, "A", true)
	_, err = r.l.Blank()
	must(t, err)
	mSignIn(r, acctB, "B", true)
	_, err = r.l.Use(acctA)
	must(t, err)
	cloneTree(t, starts["onA"], starts["out"], false)
	_, err = openRig(t, starts["out"]).l.Blank()
	must(t, err)
	cloneTree(t, starts["onA"], starts["logout"], false)
	mUse(openRig(t, starts["logout"]), "signed-out", acctA)
	cloneTree(t, starts["onA"], starts["onAi"], false)
	ri := openRig(t, starts["onAi"])
	mSignIn(ri, acctA, "Ai", true)
	must(t, os.RemoveAll(filepath.Join(ri.data, "IndexedDB")))

	for ki, k := range mKinds {
		for ci, cut := range mCuts {
			cutRoot := filepath.Join(top, fmt.Sprintf("c%d%d", ki, ci))
			t.Run(k.name+" "+cut, func(t *testing.T) {
				t.Parallel()
				cloneTree(t, starts[k.start], cutRoot, false)
				mCut(openRig(t, cutRoot), k.to, cut)
				if cut == "commit" && !full {
					// Byte for byte the state "flipped" leaves (mCut checked
					// its journal), but for the stage's name.
					return
				}
				heldConfigChangesNothing(t, cutRoot, cutRoot+"h")
				// A Recover that stopped part way, before the app was used: late
				// (held at Local Storage, the last it puts back), where the
				// switch's own undo (rollback-failed) and one after the app was
				// used stop early (held at the cookies).
				before := cutRoot + "r"
				cloneTree(t, cutRoot, before, false)
				hasBefore := cut != "rollback-failed" && partialRecover(openRig(t, before), mLS)
				// One killed between its note and its write of config.json (a
				// note left after a write that failed goes again: see above).
				crash, hasCrash := cutRoot+"k", false
				if cut == "flipped" || cut == "commit" {
					cloneTree(t, cutRoot, crash, false)
					hasCrash = crashedRecover(openRig(t, crash))
				}
				// The states of this cut in turn: one in mEvery gets a next
				// switch, each of mSwitches in turn (from a place of its own in
				// each cut).
				slot := ki + ci
				for ui, undo := range mUndo {
					switch {
					case undo == "before" && !hasBefore, undo == "after" && cut == "rollback-failed", undo == "crash" && !hasCrash:
						continue
					}
					for si, since := range mSince {
						if undo == "after" && since == "nothing" {
							continue // the same as a Recover that stopped before
						}
						for kpi, keep := range mKeep {
							if !mKindHas(ki, since, keep, full) || !all && !mSampled(ki, ci, ui, si, kpi) {
								continue
							}
							acts := []string{"recover", "settle"}
							switch {
							case full:
								acts = append(acts, mSwitches...)
							case slot%mEvery == 0:
								// By default a cut has fewer states: the turn is moved on
								// by the kind and the cut too, so each switch still meets
								// every kind, cut and state.
								i := slot / mEvery
								if !all {
									i += ki + ci
								}
								acts = append(acts, mSwitches[i%len(mSwitches)])
							}
							slot++
							heldAt := si + 2*kpi + ui // which action runs with which file held (mHeldCells)
							t.Run(undo+" "+since+" "+keep, func(t *testing.T) {
								t.Parallel()
								state, err := os.MkdirTemp(top, "s")
								must(t, err)
								t.Cleanup(func() { os.RemoveAll(state) })
								base := cutRoot
								switch undo {
								case "before":
									base = before
								case "crash":
									base = crash
								}
								cloneTree(t, base, state, false)
								r := openRig(t, state)
								ended := mUse(r, since, k.from)
								if undo == "after" && !partialRecover(r, filepath.Join("Network", "Cookies")) {
									t.Skip("a Recover after this does not stop part way")
								}
								switch keep {
								case "damaged":
									must(t, os.Remove(filepath.Join(r.l.Store, k.from, "meta.json")))
								case "off":
									_, err := r.l.turnOff()
									must(t, err)
								}
								s0 := look(r)
								if !s0.pending {
									t.Fatal("setup: no switch is pending")
								}
								mu.Lock()
								states++
								mu.Unlock()
								c := mCtx{from: k.from, off: keep == "off", ended: ended}
								report := func(got []mGot) {
									for _, g := range got {
										note(g.action, g.outcome)
										for _, p := range g.problems {
											t.Errorf("%s: %s (%s)", g.action, p, g.outcome)
										}
									}
								}
								for _, a := range acts {
									cell, err := os.MkdirTemp(top, "a")
									must(t, err)
									cloneTree(t, state, cell, true)
									report(mAct(openRig(t, cell), a, c, s0))
									os.RemoveAll(cell)
								}
								// Again with a file held (mHeldCells): one copy of the
								// state for as long as they change nothing (a copy is
								// made again after one that did).
								held := ""
								for _, h := range mHeldCells(full, acts, heldAt) {
									if held == "" {
										held, err = os.MkdirTemp(top, "h")
										must(t, err)
										cloneTree(t, state, held, true)
									}
									c.hold = h.file
									got := mAct(openRig(t, held), h.action, c, s0)
									report(got)
									if len(got) > 0 && got[0].changed {
										os.RemoveAll(held)
										held = ""
									}
								}
								if held != "" {
									os.RemoveAll(held)
								}
							})
						}
					}
				}
			})
		}
	}
}

// openRig is a rig over the folder root as it is, without changing the
// environment (so it runs in parallel): the app's folder is named to it.
func openRig(t *testing.T, root string) *rig {
	t.Helper()
	r := &rig{t: t, root: root, data: filepath.Join(root, "Claude-Data", "claude_test"),
		paths: app.Paths{Root: filepath.Join(root, "baton")}}
	must(t, os.MkdirAll(r.paths.Root, 0o700))
	r.l = New(r.paths, testAccounts())
	r.l.dataFn = func() string { return r.data }
	r.l.Running = func() bool { return false }
	r.l.UserHome = filepath.Join(root, "user", ".claude")
	r.l.Elevated = func() bool { return false }
	r.l.wait, r.l.appLockWait = 300*time.Millisecond, 200*time.Millisecond
	r.l.Now = func() time.Time { return time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC) }
	return r
}

// cloneTree copies the folder src to dst as it is, every file's and
// folder's time too, so the stamps a journal holds still read the same.
// link: each file is a second name of the one in src instead of a copy
// (baton never writes a file in place, only new ones it renames), and the
// lock files, made again on demand, are left out.
func cloneTree(t *testing.T, src, dst string, link bool) {
	t.Helper()
	type when struct {
		p   string
		mod time.Time
	}
	var times []when
	must(t, filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			fi, err := d.Info()
			if err != nil {
				return err
			}
			times = append(times, when{out, fi.ModTime()})
			return os.MkdirAll(out, 0o700)
		}
		if link {
			if strings.HasSuffix(p, ".lock") {
				return nil
			}
			return os.Link(p, out)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		times = append(times, when{out, fi.ModTime()})
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o600)
	}))
	// Deepest first: making what is inside a folder changes its time.
	for i := len(times) - 1; i >= 0; i-- {
		must(t, os.Chtimes(times[i].p, times[i].mod, times[i].mod))
	}
}

// mLS and mIDB are the files of Local Storage and of IndexedDB in a login
// set here.
var (
	mLS  = filepath.Join("Local Storage", "leveldb", "000003.log")
	mIDB = filepath.Join("IndexedDB", "https_claude.ai_0.indexeddb.leveldb", "000003.log")
)

// mPaths are the files of a login set here: one in each item, which says
// whose sign-in the item is (items move whole).
var mPaths = []string{
	mLS,
	filepath.Join("Session Storage", "000003.log"),
	mIDB,
	filepath.Join("WebStorage", "QuotaManager"),
	filepath.Join("Network", "Cookies"),
	filepath.Join("Network", "Cookies-journal"),
}

// mNeed are the files a sign-in has at the least: all of mPaths, but for
// Ai (A's in the noidb kinds), which has no IndexedDB. Run since, it may
// have made one, which is then its own as well.
func mNeed(ses string) []string {
	if base(ses) == "Ai" {
		return slices.DeleteFunc(slices.Clone(mPaths), func(p string) bool { return p == mIDB })
	}
	return mPaths
}

// mFiles are the files of a login set signed in as tag, each of them its
// own: rel|tag. A file of one sign-in is told from another's by its tag.
func mFiles(tag string) map[string]string {
	out := map[string]string{}
	for _, p := range mPaths {
		out[p] = filepath.ToSlash(p) + "|" + tag
	}
	return out
}

// mSignIn makes the app signed in to acct as tag, as the app leaves it;
// named false: quit before it wrote which account (no lastKnownAccountUuid).
func mSignIn(r *rig, acct, tag string, named bool) {
	r.t.Helper()
	r.clearItems()
	for p, c := range mFiles(tag) {
		r.write(p, c)
	}
	l := loginOf(acct, tag)
	if !named {
		l = append(l[:2:2], l[3:]...)
	}
	r.writeCfg(appCfg(l, "\n"))
}

// mCut makes a Use (to acct) or a Blank (to "") stop at cut.
func mCut(r *rig, to, cut string) {
	t := r.t
	t.Helper()
	var hold func()
	want := errCrash
	switch cut {
	case "moving-some":
		r.stopAt("move "+items[2].name, errCrash)
	case "moving-all":
		r.stopAt("config", errCrash)
	case "before-flip":
		r.stopAt("flip", errCrash)
	case "flip-unnoted":
		// The journal held open while config.json is written: the note that
		// it landed cannot be written after it.
		r.l.failAt = func(s string) error {
			switch s {
			case "flip":
				hold = holdFile(t, filepath.Join(stages(r.data, true)[0], "journal.json"))
			case "verify":
				hold()
				return errCrash
			}
			return nil
		}
	case "flipped":
		r.stopAt("verify", errCrash)
	case "commit":
		r.stopAt("commit", errCrash)
	case "rollback-failed":
		// The look after the flip fails, and the switch's own undo stops on
		// a file another program holds, once config.json is back.
		r.l.failAt = func(s string) error {
			if s == "verify" {
				hold = holdPutBack(r)
				return errTest
			}
			return nil
		}
	}
	var err error
	if to == "" {
		_, err = r.l.Blank()
	} else {
		_, err = r.l.Use(to)
	}
	r.l.failAt = nil
	if hold != nil {
		hold()
	}
	if cut == "rollback-failed" {
		wantCode(t, "setup", err, CodeSwitchUnrestored)
	} else if err != want {
		t.Fatalf("setup: %v", err)
	}
	s := stages(r.data, true)
	if len(s) != 1 {
		t.Fatalf("setup: %d stages", len(s))
	}
	j, err := readJournal(s[0])
	must(t, err)
	now, err := os.ReadFile(filepath.Join(r.data, "config.json"))
	must(t, err)
	h := sum(now)
	ok := false
	switch cut {
	case "moving-some", "moving-all":
		ok = j.Phase == phaseMoving
	case "before-flip":
		ok = j.Phase == phaseConfig && !j.Flipped && h == j.CfgBefore
	case "flip-unnoted":
		ok = j.Phase == phaseConfig && !j.Flipped && h == j.CfgAfter
	case "flipped", "commit":
		ok = j.Phase == phaseConfig && j.Flipped && h == j.CfgAfter
	case "rollback-failed":
		ok = j.Phase == phaseConfig && j.Flipped && h == j.CfgBefore
	}
	if !ok {
		t.Fatalf("setup: %s: %+v", cut, j)
	}
}

// holdFile opens p as another program may (an antivirus, the indexer):
// shared for reading and writing, not for delete. release closes it.
func holdFile(t *testing.T, p string) (release func()) {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	must(t, err)
	h, err := windows.CreateFile(u, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	must(t, err)
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(release)
	return release
}

// holdExclusive opens p as a program that does not share it for reading
// does (share mode 0): no one else can open it at all until release. nil
// when there is no such file.
func holdExclusive(t *testing.T, p string) (release func()) {
	t.Helper()
	u, err := windows.UTF16PtrFromString(p)
	must(t, err)
	h, err := windows.CreateFile(u, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	must(t, err)
	var once sync.Once
	release = func() { once.Do(func() { windows.CloseHandle(h) }) }
	t.Cleanup(release)
	return release
}

// holdPutBack holds the cookies a put back has to replace (the app's) or
// move (the stage's), so it stops there: nil when there are none.
func holdPutBack(r *rig) func() { return holdItem(r, filepath.Join("Network", "Cookies")) }

// holdItem holds the file rel of an item a put back has to replace (the
// app's) or move (the stage's), so it stops there: nil when there is none.
// A put back goes from the last item to the first: held at the cookies, it
// stops early (only Cookies-journal is back); held at Local Storage, late
// (all but it are back).
func holdItem(r *rig, rel string) func() {
	p := filepath.Join(r.data, rel)
	if !exists(p) {
		s := stages(r.data, true)
		if len(s) != 1 {
			return nil
		}
		if p = filepath.Join(s[0], "old", rel); !exists(p) {
			return nil
		}
	}
	return holdFile(r.t, p)
}

// partialRecover runs a Recover that stops part way on the held file rel:
// whether it did (the switch is still pending, not undone).
func partialRecover(r *rig, rel string) bool {
	release := holdItem(r, rel)
	if release == nil {
		return false
	}
	defer release()
	_, err := r.l.Recover()
	return Code(err) == CodeSwitchUnrestored && len(stages(r.data, true)) == 1
}

// crashedRecover runs a Recover killed between its note that it writes
// config.json back and the write: whether it was (the note is there, and
// config.json is as the switch wrote it).
func crashedRecover(r *rig) bool {
	r.l.failAt = func(s string) error {
		if s == "restore config" {
			return errCrash
		}
		return nil
	}
	_, err := r.l.Recover()
	r.l.failAt = nil
	s := stages(r.data, true)
	if Code(err) != CodeSwitchUnrestored || len(s) != 1 || !exists(filepath.Join(s[0], restoringName)) {
		return false
	}
	j, err := readJournal(s[0])
	now, cerr := os.ReadFile(filepath.Join(r.data, "config.json"))
	return err == nil && cerr == nil && sum(now) == j.CfgAfter
}

// heldConfigChangesNothing: a Recover of the state at root (a copy of it, at
// at) that cannot write config.json (another program holds it) refuses and
// changes nothing, the note that it began to included, when it has
// config.json to write back (the flip is in place).
func heldConfigChangesNothing(t *testing.T, root, at string) {
	t.Helper()
	cloneTree(t, root, at, false)
	r := openRig(t, at)
	s := stages(r.data, true)
	cp := filepath.Join(r.data, "config.json")
	j, err := readJournal(s[0])
	must(t, err)
	now, err := os.ReadFile(cp)
	must(t, err)
	if sum(now) != j.CfgAfter {
		return
	}
	s0 := look(r)
	release := holdFile(t, cp)
	_, err = r.l.Recover()
	release()
	s1 := look(r)
	if Code(err) != CodeSwitchUnrestored {
		t.Errorf("a Recover that cannot write config.json: %v", err)
	}
	if !mapsEqual(s0.data, s1.data) || !mapsEqual(s0.store, s1.store) {
		var diff []string
		for p, c := range s1.data {
			if s0.data[p] != c {
				diff = append(diff, p)
			}
		}
		for p := range s0.data {
			if _, ok := s1.data[p]; !ok {
				diff = append(diff, "gone: "+p)
			}
		}
		slices.Sort(diff)
		t.Errorf("a Recover that could not write config.json changed the app's folder: %v", diff)
	}
}

// mUse is what the app went through since, opened outside baton. ended are
// the sign-ins (bases, comma separated) the app's own Log out ended on
// Claude's servers, if any: a copy of one no longer signs anyone in.
func mUse(r *rig, since, from string) (ended string) {
	t := r.t
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.data, "config.json"))
	must(t, err)
	cfg, why := readAppConfig(b)
	if cfg == nil {
		t.Fatalf("setup: config.json: %s", why)
	}
	cookies := filepath.Join("Network", "Cookies")
	switch since {
	case "ran", "unknown":
		// A setting of its own changed, and the sign-in it has renewed (its
		// tokens and cookies), which makes the site storage it lacks (an
		// item the switch, or a put back, took out of the app); signed out,
		// it makes cookies and site storage of its own. An update of the app
		// may write it in a shape baton does not know: a token key of a new
		// name, beside the two it knows.
		login, v3 := cfg.obj.login(), `""`
		if cfg.token {
			tag, _ := sixTag(cfg.obj)
			login = loginOf(cfg.account, tag+"~r")
			if cfg.account == "" {
				login = append(login[:2:2], login[3:]...)
			}
			v3 = `"sealed3-` + tag + `~r-1"`
			r.write(cookies, filepath.ToSlash(cookies)+"|"+tag+"~r")
			for _, p := range mPaths {
				if !exists(filepath.Join(r.data, p)) {
					r.write(p, filepath.ToSlash(p)+"|"+tag+"~r")
				}
			}
		} else {
			r.write(cookies, filepath.ToSlash(cookies)+"|out")
			for _, p := range mPaths {
				if !exists(filepath.Join(r.data, p)) {
					r.write(p, filepath.ToSlash(p)+"|out")
				}
			}
		}
		if since == "unknown" {
			login = append(login, kv{"oauth:tokenCacheV3", []byte(v3)})
		}
		if cfg.token || since == "unknown" {
			b = cfg.obj.splice(login)
		}
		if !strings.Contains(string(b), `"locale": "ko-KR"`) {
			t.Fatal("setup: no setting to change")
		}
		r.writeCfg([]byte(strings.Replace(string(b), `"locale": "ko-KR"`, `"locale": "en-US"`, 1)))
	case "signed-out":
		// The app's own Log out: the tokens go, the account stays named,
		// and it makes cookies and site storage of its own.
		if cfg.token {
			tag, _ := sixTag(cfg.obj)
			ended = base(tag)
			var l []kv
			for _, k := range cfg.obj.login() {
				if slices.Contains(tokenKeys, k.key) {
					k.raw = []byte(`""`)
				}
				l = append(l, k)
			}
			r.writeCfg(cfg.obj.splice(l))
		}
		r.clearItems()
		ls := filepath.Join("Local Storage", "leveldb", "000003.log")
		r.write(cookies, filepath.ToSlash(cookies)+"|out")
		r.write(ls, filepath.ToSlash(ls)+"|out")
	case "from-again":
		mSignIn(r, from, "An", true)
	case "from-again-out":
		// Signed in to From again, then its own Log out: it names From, with
		// no token, as after a Log out of the sign-in the switch left. That
		// Log out ended An; and the one before it, when the app was on From's
		// older sign-in (signing in again took a Log out of it first).
		mSignIn(r, from, "An", true)
		o := r.cfg()
		var l []kv
		for _, k := range o.login() {
			if slices.Contains(tokenKeys, k.key) {
				k.raw = []byte(`""`)
			}
			l = append(l, k)
		}
		r.writeCfg(o.splice(l))
		r.clearItems()
		r.write(cookies, filepath.ToSlash(cookies)+"|out")
		r.write(mLS, filepath.ToSlash(mLS)+"|out")
		ended = "An"
		if cfg.token && cfg.account == from {
			tag, _ := sixTag(cfg.obj)
			ended += "," + base(tag)
		}
	case "c-named":
		mSignIn(r, acctC, "C", true)
	case "c-unnamed":
		mSignIn(r, acctC, "C", false)
	case "c-unknown":
		mSignIn(r, acctC, "C", true)
		o := r.cfg()
		r.writeCfg(o.splice(append(o.login(), kv{"oauth:tokenCacheV3", []byte(`"sealed3-C-1"`)})))
	}
	return ended
}

var sideRx = regexp.MustCompile(`"t": "(\S+) <title>`)

// sixTag is the tag of the sign-in the six values of o are (every one of
// them that names one); mixed: they are not all one sign-in's.
func sixTag(o *object) (tag string, mixed bool) {
	var tags []string
	for _, k := range o.login() {
		var s string
		switch k.key {
		case "oauth:tokenCache", "oauth:tokenCacheV2", "bootSidebarPlaceholder":
			if json.Unmarshal(k.raw, &s) != nil || s == "" {
				continue
			}
			s = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(s, "sealed2-"), "sealed-"), "-1"), " placeholder")
		case "bootSideSessions":
			m := sideRx.FindSubmatch(k.raw)
			if m == nil {
				continue
			}
			s = string(m[1])
		default:
			continue
		}
		tags = append(tags, s)
	}
	for _, s := range tags {
		if base(s) != base(tags[0]) {
			return tags[0], true
		}
	}
	if len(tags) == 0 {
		return "", false
	}
	return tags[0], false
}

// hasToken: any "oauth:" member of o, of a name baton knows or not, holds a
// token (a string that is not empty).
func hasToken(o *object) bool {
	for _, m := range o.members {
		var s string
		if strings.HasPrefix(strings.ToLower(m.key), "oauth:") && json.Unmarshal(o.raw(m), &s) == nil && s != "" {
			return true
		}
	}
	return false
}

// base is the sign-in a tag is: a renewal (~r) is the same sign-in.
func base(tag string) string {
	b, _, _ := strings.Cut(tag, "~")
	return b
}

// acctOf is the account of a sign-in (or of a renewal of one).
func acctOf(ses string) string {
	switch base(ses) {
	case "A", "An", "Ai":
		return acctA
	case "B":
		return acctB
	case "C":
		return acctC
	}
	return ""
}

// seen is what a look finds.
type seen struct {
	data, store map[string]string // every file and folder, for I4

	app     string            // the sign-in the app has, whole ("" none)
	half    string            // why the app is half switched ("" when it is not)
	tokens  string            // the sign-in of the app's tokens ("" none)
	tag     string            // their tag (a renewal is told apart)
	items   []string          // the sign-ins of the app's items
	named   string            // the account its config.json names
	files   map[string]string // the files of its login set
	nItems  int               // how many there are
	unknown bool              // config.json is in a shape baton does not know

	kept      map[string]string            // account -> the sign-in its kept copy is, when whole
	keptTag   map[string]string            // and the tag of its tokens (a renewal is told apart)
	keptFiles map[string]map[string]string // and its files
	keptHalf  []mHalf                      // kept copies baton takes for whole that mix sign-ins

	staged      []string            // the sign-ins (tags) pending stages hold whole (with what is still in the app)
	stagedFiles []map[string]string // and the files of each
	pending     bool
	stages      int
	journal     *journal
	marker      bool   // a restore began to write config.json back
	left        bool   // the switch left a sign-in (the config.json it read held a token)
	leftTag     string // and the tag of that sign-in
}

// mHalf is a kept copy baton takes for whole that is not one sign-in's.
type mHalf struct {
	acct  string            // the account it is kept as
	tag   string            // the sign-in of its tokens
	mixed bool              // its six values are two sign-ins'
	files map[string]string // its login set
}

func (k mHalf) String() string {
	return fmt.Sprintf("%s: tokens of %s, items of %v (%d files)", k.acct[:8], k.tag, fullTags(k.files), len(k.files))
}

// fullTags are the tags of a login set's files, renewals told apart.
func fullTags(files map[string]string) []string {
	var tags []string
	for _, c := range files {
		if t := fileTag(c); !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	return tags
}

// readAll is every file under dir by its path, with its content (a folder:
// "dir"), each read once.
func readAll(t *testing.T, dir string) map[string]string {
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
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	}))
	return out
}

// setIn are the files of a login set under the folder prefix of all.
func setIn(all map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for _, it := range items {
		at := filepath.Join(prefix, it.name)
		for p, c := range all {
			if c != "dir" && (p == at || strings.HasPrefix(p, at+string(filepath.Separator))) {
				rel := p
				if prefix != "" {
					rel, _ = filepath.Rel(prefix, p)
				}
				out[rel] = c
			}
		}
	}
	return out
}

// fileTag is the tag of the sign-in a file of a login set is ("?" none).
func fileTag(c string) string {
	if i := strings.LastIndex(c, "|"); i >= 0 {
		return c[i+1:]
	}
	return "?"
}

// filesTags are the sign-ins (bases) of a login set's files, and how many
// there are.
func filesTags(files map[string]string) (tags []string, n int) {
	for _, c := range files {
		tag := base(fileTag(c))
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
		n++
	}
	slices.Sort(tags)
	return tags, n
}

// hasAll: files has each of paths.
func hasAll(files map[string]string, paths []string) bool {
	for _, p := range paths {
		if _, ok := files[p]; !ok {
			return false
		}
	}
	return true
}

// wholeBase: a login set of one sign-in, ses (its base), with every file
// that sign-in has (mNeed), as it was or renewed since. What putting a
// stage back gives is told so: its tokens are those config.json has then.
func wholeBase(files map[string]string, ses string) bool {
	tags, _ := filesTags(files)
	return ses != "" && len(tags) == 1 && tags[0] == base(ses) && hasAll(files, mNeed(ses))
}

// mCookies is the file of the cookies in a login set here.
var mCookies = filepath.Join("Network", "Cookies")

// whole: a login set of the sign-in whose tokens are tag (wholeBase), none
// of its files newer than those tokens, and its cookies theirs: the app
// renews its tokens and its cookies together (mUse "ran"), so a renewal's
// tokens over the older cookies are two sign-ins of one account mixed.
func whole(files map[string]string, tag string) bool {
	if !wholeBase(files, tag) || fileTag(files[mCookies]) != tag {
		return false
	}
	for _, c := range files {
		if ft := fileTag(c); ft != tag && !strings.HasPrefix(tag, ft+"~") {
			return false
		}
	}
	return true
}

// renewalOf: tag is was or a renewal of it.
func renewalOf(tag, was string) bool { return tag == was || strings.HasPrefix(tag, was+"~") }

// covers: files holds every file of was, each as it was or renewed since
// (an older file never stands in for a renewal's). why: the first that is
// not.
func covers(files, was map[string]string) (ok bool, why string) {
	paths := slices.Sorted(maps.Keys(was))
	for _, p := range paths {
		c, ok := files[p]
		if !ok {
			return false, filepath.ToSlash(p) + " is gone"
		}
		if !renewalOf(fileTag(c), fileTag(was[p])) {
			return false, fmt.Sprintf("%s is %s's, not %s's", filepath.ToSlash(p), fileTag(c), fileTag(was[p]))
		}
	}
	return true, ""
}

func look(r *rig) *seen {
	t := r.t
	t.Helper()
	s := &seen{data: readAll(t, r.data), store: readAll(t, r.l.Store), kept: map[string]string{}, keptTag: map[string]string{},
		keptFiles: map[string]map[string]string{}}
	s.files = setIn(s.data, "")
	if b, ok := s.data["config.json"]; !ok {
		s.half = "no config.json"
	} else if o, why := parseObject([]byte(b)); o == nil {
		s.half = "config.json: " + why
	} else {
		// Read as the app would, also in a shape baton does not know.
		cfg, _ := readAppConfig([]byte(b))
		s.unknown, s.named = cfg == nil, rawAccount([]byte(b))
		files := s.files
		s.items, s.nItems = filesTags(files)
		if hasToken(o) {
			tag, mixed := sixTag(o)
			s.tokens, s.tag = base(tag), tag
			switch {
			case mixed:
				s.half = "its six values are two sign-ins'"
			case s.named != "" && s.named != acctOf(s.tokens):
				s.half = "it names another account than its tokens'"
			case !whole(files, s.tag):
				s.half = fmt.Sprintf("tokens of %s with items of %v (%d files)", s.tag, fullTags(files), len(files))
			default:
				s.app = s.tokens
			}
		}
	}
	for _, acct := range []string{acctA, acctB, acctC} {
		dir := filepath.Join(r.l.Store, acct)
		if s.store[acct] != "dir" || checkFolder(dir, acct) != "" {
			continue
		}
		keys, _ := readKeys([]byte(s.store[filepath.Join(acct, "keys.json")]))
		o, _ := parseObject(keysFile(keys))
		tag, mixed := sixTag(o)
		files := setIn(s.store, acct)
		if !mixed && acctOf(tag) == acct && whole(files, tag) {
			s.kept[acct], s.keptTag[acct], s.keptFiles[acct] = base(tag), tag, files
		} else {
			s.keptHalf = append(s.keptHalf, mHalf{acct, tag, mixed, files})
		}
	}
	all := stages(r.data, false)
	s.stages = len(all)
	for _, st := range all {
		if !pending(st) {
			continue
		}
		s.pending = true
		j, err := readJournal(st)
		must(t, err)
		s.journal = j
		name := filepath.Base(st)
		s.marker = s.data[filepath.Join(name, restoringName)] != ""
		old, _ := readAppConfig([]byte(s.data[filepath.Join(name, "old", "config.json")]))
		s.left = old == nil || old.token // as leftSignIn reads it
		if old == nil || !old.token {
			continue
		}
		tag, _ := sixTag(old.obj)
		s.leftTag = tag
		// What putting it back would give: each item set aside, else the
		// app's own where it was never moved out (or was put back); and of
		// an item it did not have, what the app made for it since (a put
		// back took the switch's copy out, then the app ran on it).
		aside := setIn(s.data, filepath.Join(name, "old"))
		files := map[string]string{}
		for _, it := range j.Items {
			from := s.files
			if it.HadOld && s.data[filepath.Join(name, "old", it.Name)] != "" {
				from = aside
			}
			for p, c := range from {
				if p != it.Name && !strings.HasPrefix(p, it.Name+string(filepath.Separator)) {
					continue
				}
				if it.HadOld || base(fileTag(c)) == base(tag) {
					files[p] = c
				}
			}
		}
		if wholeBase(files, tag) {
			s.staged = append(s.staged, tag)
			s.stagedFiles = append(s.stagedFiles, files)
		}
	}
	return s
}

// mAt is a place a sign-in is whole in, with its files there.
type mAt struct {
	place string
	files map[string]string
}

// where are the sign-ins whole in s, by tag (a renewal is told apart from
// the older copy), each with where.
func (s *seen) where() map[string][]mAt {
	out := map[string][]mAt{}
	if s.app != "" {
		out[s.tag] = append(out[s.tag], mAt{"app", s.files})
	}
	for acct, tag := range s.keptTag {
		out[tag] = append(out[tag], mAt{"kept " + acct[:8], s.keptFiles[acct]})
	}
	for i, tag := range s.staged {
		out[tag] = append(out[tag], mAt{"stage", s.stagedFiles[i]})
	}
	return out
}

// foundAfter: the sign-in ses, whole in ats before an action, is whole after
// it (after) as it was or renewed: each place it was in has a place after
// with every file of it, each as it was or renewed (covers). why: the
// nearest miss.
func foundAfter(ses string, ats []mAt, after map[string][]mAt) (ok bool, why string) {
	for _, b := range ats {
		found := false
		for k, as := range after {
			if !renewalOf(k, ses) {
				continue
			}
			for _, a := range as {
				if ok, w := covers(a.files, b.files); ok {
					found = true
				} else if why == "" {
					why = fmt.Sprintf("; %s in %s is not as it was in %s: %s", k, a.place, b.place, w)
				}
			}
		}
		if !found {
			return false, why
		}
	}
	return true, ""
}

func foundOK(ok bool, _ string) bool { return ok }

// places are where ats are.
func places(ats []mAt) []string {
	var out []string
	for _, a := range ats {
		out = append(out, a.place)
	}
	return out
}

// mixOK: the documented answer where the app names From with a token, the
// switch left a sign-in of From's (left), and the flip is not known to be
// in place: From's items may go back under the app's tokens, a mix of two
// sign-ins of From's account at worst. A switch that left no sign-in (from
// an app with no token) has no items of From's to put back under them.
func mixOK(s *seen, from string) bool {
	if from == "" || s.journal == nil || !s.left || s.named != from || s.tokens == "" || acctOf(s.tokens) != from {
		return false
	}
	j := s.journal
	return j.Phase != phaseConfig || !j.Flipped || s.marker
}

// mixOneAccount: a login set (files) under the tokens of tag mixes only
// sign-ins of From's account, with every file one of them has (the fewest):
// the documented mix (mixOK) makes no other.
func mixOneAccount(tag string, files map[string]string, from string) bool {
	tags, _ := filesTags(files)
	if acctOf(tag) != from || len(tags) == 0 {
		return false
	}
	need := mPaths
	for _, ses := range tags {
		if acctOf(ses) != from {
			return false
		}
		if n := mNeed(ses); len(n) < len(need) {
			need = n
		}
	}
	return hasAll(files, need)
}

// halfOK: the app is half switched only as mixOK allows: under the tokens it
// had, sign-ins of From's account only (mixOneAccount).
func halfOK(s1, s0 *seen, from string) bool {
	return mixOK(s0, from) && s1.tokens == s0.tokens && mixOneAccount(s1.tag, s1.files, from)
}

// keptHalfOK: a kept copy mixes sign-ins only as mixOK allows: the next
// switch kept the app's documented mix as From's, From's account only.
func keptHalfOK(k mHalf, s0 *seen, from string) bool {
	return mixOK(s0, from) && k.acct == from && !k.mixed && mixOneAccount(k.tag, k.files, from)
}

// mixedInto: the documented mix (mixOK) took the sign-in ses of From's,
// whole in ats before, into a place after: the app under the tokens it had,
// or the kept copy of From. That place mixes sign-ins of From's account only
// (mixOneAccount), and holds ses's tokens or a file of ses (as it was or
// renewed): From's items put back over ses's (which may look whole: a
// renewal's tokens and cookies with the older site storage). It has a file
// at every place ses had one, unless that file was another sign-in's than
// the one the switch left (s0.leftTag): its items replace another's, and it
// may lack one (Ai, no IndexedDB), but what of its own the app made since,
// renewed, is never gone.
func mixedInto(s1, s0 *seen, from, ses string, ats []mAt) bool {
	in := func(tag string, files map[string]string) bool {
		if !mixOneAccount(tag, files, from) {
			return false
		}
		for _, b := range ats {
			for p, c := range b.files {
				if _, ok := files[p]; !ok && base(fileTag(c)) == base(s0.leftTag) {
					return false
				}
			}
		}
		if renewalOf(tag, ses) {
			return true
		}
		for _, c := range files {
			if renewalOf(fileTag(c), ses) {
				return true
			}
		}
		return false
	}
	if s1.tokens != "" && s1.tokens == s0.tokens && in(s1.tag, s1.files) {
		return true
	}
	if tag, ok := s1.keptTag[from]; ok && in(tag, s1.keptFiles[from]) {
		return true
	}
	return slices.ContainsFunc(s1.keptHalf, func(k mHalf) bool { return keptHalfOK(k, s0, from) && in(k.tag, k.files) })
}

var refusals = []string{CodeUnsavedCurrent, CodeDamaged, CodeUsedSince, CodeOff, CodeNotKept, CodeAlready, CodeSwitchUnrestored}

type mGot struct {
	action, outcome string
	problems        []string
	changed         bool // the app's folder or the store is not as it was
}

// mCtx is what a cell knows of how its state came to be.
type mCtx struct {
	from  string // the account the switch left ("" a signed-out app)
	off   bool   // switching is off: nothing can be kept
	ended string // the sign-ins the app's own Log out ended since, comma separated
	hold  string // a file of mHeld another program holds, not shared for reading, while the action runs
}

// mAct runs action on r (in state s0): how it went, and every rule it broke.
// "settle" is followed by TurnOff, as uninstall does (a second result).
func mAct(r *rig, action string, c mCtx, s0 *seen) []mGot {
	from, off := c.from, c.off
	g := mGot{action: action}
	bad := func(f string, a ...any) { g.problems = append(g.problems, fmt.Sprintf(f, a...)) }
	var release func()
	if c.hold != "" {
		g.action += ", " + c.hold + " held"
		p := filepath.Join(r.data, "config.json")
		if c.hold != "config.json" {
			s := stages(r.data, true)
			if len(s) != 1 {
				r.t.Fatalf("setup: %d stages", len(s))
			}
			p = filepath.Join(s[0], c.hold)
		}
		if release = holdExclusive(r.t, p); release == nil {
			return nil
		}
	}
	var ok bool
	var res Switched
	var err error
	switch action {
	case "recover":
		ok, err = r.l.Recover()
	case "settle":
		ok, err = r.l.Settle()
	case "use A", "use B", "use C":
		res, err = r.l.Use(map[string]string{"use A": acctA, "use B": acctB, "use C": acctC}[action])
	case "blank":
		res, err = r.l.Blank()
	}
	if release != nil {
		release()
	}
	s1 := look(r)
	g.changed = !mapsEqual(s0.data, s1.data) || !mapsEqual(s0.store, s1.store)
	g.outcome = "ok"
	if err != nil {
		g.outcome = Code(err)
		if g.outcome == "" {
			g.outcome = "error " + err.Error()
		}
		if d := detailOf(err); d != "" {
			g.outcome += " (" + d + ")"
		}
	}
	if res.Recovered {
		g.outcome = "undone, then " + g.outcome
	}
	settle := action == "settle"

	// I4: a refusal changes nothing. While a file it reads is held, Settle
	// may refuse too ("try again"); I3 holds once it is not.
	if err != nil && !res.Recovered {
		if settle && c.hold == "" {
			bad("Settle did not end the stage")
		}
		if !slices.Contains(refusals, Code(err)) {
			bad("not a refusal")
		}
		if g.changed {
			bad("refused, yet changed something")
		}
		return []mGot{g}
	}
	// I3: Settle always ends it.
	if settle && !ok {
		bad("Settle did not end the stage")
	}
	if s1.pending {
		bad("a switch is still pending")
	}
	if s1.stages > 0 && c.hold == "" {
		// Held, a file of the stage may not go: what is left is only a copy.
		bad("a stage is left")
	}
	// I1: never half switched, the app nor a kept copy.
	if s1.half != "" && !halfOK(s1, s0, from) {
		bad("the app is half switched: %s", s1.half)
	}
	for _, k := range s1.keptHalf {
		if !keptHalfOK(k, s0, from) {
			bad("a kept copy is half switched: %s", k)
		}
	}
	// I2: no sign-in lost, nor any file of one.
	before, after := s0.where(), s1.where()
	for ses, ats := range before {
		// Whole after, as it was or renewed (the app ran on it since), with
		// every file it had, each as it was or renewed: neither the older
		// copy nor an older file stands in for a renewal.
		found, why := foundAfter(ses, ats, after)
		if found {
			continue
		}
		staged := !slices.ContainsFunc(ats, func(a mAt) bool { return a.place != "stage" })
		switch {
		case settle && off && staged:
			// Leave it as it is, while nothing can be kept: what the switch
			// set aside goes, as the window says before it asks.
		case mixOK(s0, from) && acctOf(ses) == from && mixedInto(s1, s0, from, ses, ats):
			// The documented mix: From's items under the app's tokens, two
			// sign-ins of From's account made one (the next switch keeps it
			// as From's), with nothing of another account, and no file of
			// either gone. From stays signed in.
		case slices.Contains(strings.Split(c.ended, ","), base(ses)):
			// Ended on Claude's servers by the app's own Log out: no copy of
			// it signs anyone in any more.
		case s0.app != "" && base(ses) != s0.app && acctOf(ses) == acctOf(s0.app) && foundOK(foundAfter(s0.tag, before[s0.tag], after)):
			// Superseded by the app's own newer sign-in of that account.
		case off && action == "recover" && ses == "B" && s0.tag == "B" && slices.Equal(places(ats), []string{"app"}):
			// The copy of B the switch moved in, unused since, whose kept
			// copy TurnOff deleted: putting the switch back takes it out.
		default:
			bad("the sign-in %s (%v) is lost%s", ses, places(ats), why)
		}
	}
	if settle && s0.app != "" && !sameSignIn(s1, s0) && !mixOK(s0, from) {
		bad("Settle did not leave the app's sign-in %s as it is (now %q, %d files)", s0.tag, s1.tag, s1.nItems)
	}
	// What each action is for.
	switch {
	case action == "recover":
		if !ok {
			bad("Recover undid nothing")
		}
		if s1.tokens != "" && acctOf(s1.tokens) != from {
			bad("Recover left the app on %s, not on what it was on", s1.tokens)
		}
	case strings.HasPrefix(action, "use") && err == nil:
		if want := map[string]string{"use A": acctA, "use B": acctB, "use C": acctC}[action]; s1.app == "" || acctOf(s1.app) != want {
			bad("Use did not leave the app on its account: %q", s1.app)
		}
	case action == "blank" && err == nil:
		if s1.tokens != "" || s1.nItems != 0 {
			bad("Blank left a sign-in in the app")
		}
	}
	if !settle {
		return []mGot{g}
	}
	// Uninstall: Settle, then TurnOff, which deletes every kept copy.
	u := mGot{action: "uninstall", outcome: "ok"}
	if _, err := r.l.turnOff(); err != nil {
		u.outcome = "error " + err.Error()
		u.problems = append(u.problems, "turn off failed")
		return []mGot{g, u}
	}
	s2 := look(r)
	if !mapsEqual(outsideStages(s1.data), outsideStages(s2.data)) {
		u.problems = append(u.problems, "TurnOff changed the app")
	}
	if s2.half != "" && !halfOK(s2, s0, from) {
		u.problems = append(u.problems, "the app is half switched: "+s2.half)
	}
	if s0.app != "" && !sameSignIn(s2, s0) && !mixOK(s0, from) {
		u.problems = append(u.problems, fmt.Sprintf("the app no longer has its sign-in %s (now %q, %d files)", s0.tag, s2.tag, s2.nItems))
	}
	return []mGot{g, u}
}

// sameSignIn: the app in s1 has the sign-in it has in s0, whole, renewal
// and every file of it as it was.
func sameSignIn(s1, s0 *seen) bool {
	if s1.app != s0.app || s1.tag != s0.tag {
		return false
	}
	for p, c := range s0.files {
		if s1.files[p] != c {
			return false
		}
	}
	return true
}

func detailOf(err error) string {
	var de *desktop.Error
	if errors.As(err, &de) {
		return de.Detail
	}
	return ""
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
