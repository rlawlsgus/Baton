package desktop

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Taking a conversation off the desktop app before it is deleted (Forget):
// baton's own records go, on lists the running app does not hold; anything
// of the app's that names the conversation refuses, and a refusal or a look
// changes nothing at all.

const appsConv = "f0000000-0000-4000-8000-00000000000f" // a conversation of the app's own

// forget asks Forget about conversation id.
func (r *rig) forget(id string, dry bool) ForgetResult {
	r.t.Helper()
	res, err := r.d.Forget(id, dry)
	must(r.t, err)
	return res
}

// files is every file under the test's folder (but the lock baton takes)
// and what it holds: the index, every list, the user's own folder.
func (r *rig) files() map[string]string {
	r.t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(r.root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || samePath(p, r.d.LockPath) {
			return nil
		}
		b, _ := os.ReadFile(p) // one held by the test reads as nothing, each time
		out[p] = string(b)
		return nil
	})
	return out
}

// unchanged checks that nothing under the test's folder changed since before.
func (r *rig) unchanged(before map[string]string, what string) {
	r.t.Helper()
	after := r.files()
	if maps.Equal(before, after) {
		return
	}
	for p, b := range before {
		if a, ok := after[p]; !ok || a != b {
			r.t.Errorf("%s: %s changed or went", what, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			r.t.Errorf("%s: %s turned up", what, p)
		}
	}
	r.t.FailNow()
}

// refused checks that Forget, looking and for real, refuses with exactly
// want and changes nothing.
func (r *rig) refused(id string, want ...Blocker) ForgetResult {
	r.t.Helper()
	before := r.files()
	var res ForgetResult
	for _, dry := range []bool{true, false} {
		res = r.forget(id, dry)
		if !slices.Equal(res.Blockers, want) {
			r.t.Fatalf("dry=%v: blockers %+v, want %+v", dry, res.Blockers, want)
		}
		r.unchanged(before, "a refusal")
	}
	return res
}

var (
	isBusy    = Blocker{Code: "desktop_busy"}
	isWaiting = Blocker{Code: "desktop_waiting"}
	isUnread  = Blocker{Code: "cannot_check"}
)

func appHas(detail string) Blocker { return Blocker{Code: "desktop_app", Detail: detail} }

// A session baton shows on lists the app does not hold: a look says which
// and changes nothing; Forget takes baton's records off and the second name
// with them, remembers the records (a copy the app writes back goes too),
// and leaves everything of the app's as it is.
func TestForgetTakesBatonsRecordsOff(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA, keyB)
	h := r.second(file, sid)
	// The app's own: a record of another conversation, what a delete of
	// another one left, in the list and next to the conversation.
	own := r.appRecord(keyA, "local_d0000000-0000-4000-8000-0000000000d0", appsConv)
	tomb := filepath.Join(r.placeDir(keyA), "deleted_"+sid2)
	must(t, os.WriteFile(tomb, []byte("1800000005000"), 0o600))
	marker := filepath.Join(filepath.Dir(h), sid2+".desktop-released.json")
	must(t, os.WriteFile(marker, []byte(`{"v":1,"reason":"delete"}`), 0o600))
	recB := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	writtenB, _ := os.ReadFile(recB)

	before := r.files()
	res := r.forget(strings.ToUpper(sid), true)
	if len(res.Blockers) != 0 || !slices.Equal(res.Off, []string{"a, a2", "b"}) || !slices.Equal(res.Links, []string{h}) || res.AppRunning {
		t.Fatalf("look: %+v", res)
	}
	r.unchanged(before, "a look")

	res = r.forget(sid, false)
	if len(res.Blockers) != 0 || !slices.Equal(res.Off, []string{"a, a2", "b"}) || len(res.Links) != 0 {
		t.Fatalf("forget: %+v", res)
	}
	if r.record(keyA, "local_"+sid) != nil || isFile(recB) || isFile(h) || !isFile(file) {
		t.Fatal("baton's records or the second name stayed, or baton's file went")
	}
	ix := r.index()
	if len(ix.Threads) != 0 || len(ix.Gone) != 2 || !ix.Gone[0].knows(sid) || !ix.Gone[1].knows(sid) {
		t.Fatalf("index: %+v", ix)
	}
	for p, b := range before {
		if p == own || p == tomb || p == marker {
			if got, _ := os.ReadFile(p); string(got) != b {
				t.Fatalf("the app's %s changed or went", p)
			}
		}
	}
	// The app (running unnoticed) writes baton's record back: it goes again.
	must(t, os.WriteFile(recB, writtenB, 0o600))
	must(t, r.d.Reconcile())
	if isFile(recB) {
		t.Fatal("a record of baton's written back after Forget stayed")
	}
}

// While the running app is signed in to a list that shows the session (or
// may write back one it was taken off), nothing is taken off anywhere.
func TestForgetWaitsForTheRunningApp(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.running = true // signed in to A
	r.apply(sid, file, keyA, keyB)
	if res := r.refused(sid, isBusy); !slices.Equal(res.Off, []string{"a, a2", "b"}) || !res.AppRunning {
		t.Fatalf("shown on the running app's list: %+v", res)
	}
	r.apply(sid, file, keyB) // A unticked: the running app may write it back
	if p := r.place(sid, keyA); p == nil || p.State != stateHiding {
		t.Fatalf("setup: %+v", p)
	}
	if res := r.refused(sid, isBusy); !slices.Equal(res.Off, []string{"b"}) {
		t.Fatalf("taken off the running app's list: %+v", res)
	}
	r.running = false
	must(t, r.d.Reconcile())
	if res := r.forget(sid, false); len(res.Blockers) != 0 || r.record(keyB, "local_"+sid) != nil {
		t.Fatalf("once the app is closed: %+v", res)
	}
}

// A record of the desktop app's that names the conversation anywhere, in
// any list of any of its data folders, refuses: the user deletes it there.
// Also an earlier part of one of its sessions, one half written, one too
// big for the app to list now, and the id in capitals.
func TestForgetRefusesTheAppsRecords(t *testing.T) {
	const appRec = "local_d0000000-0000-4000-8000-0000000000d1"
	cases := []struct {
		name   string
		key    string
		file   string // the record's file name ("": appRec.json)
		data   string // what it holds (%s: the id)
		detail string
	}{
		{"its own", keyA, "", `{"sessionId":"x","cliSessionId":"%s","cwd":"x"}`, "own"},
		{"in capitals", keyA, "", `{"cliSessionId":"%S"}`, "own"},
		{"archived", keyA, "", `{"cliSessionId":"%s","isArchived":true}`, "archived"},
		{"an earlier conversation", keyA, "", `{"cliSessionId":"` + appsConv + `","priorCliSessionIds":["%s"]}`, "prior"},
		{"rewound", keyA, "", `{"cliSessionId":"` + appsConv + `","rewindEdges":[{"parent":"%s","child":"` + appsConv + `"}]}`, "prior"},
		{"last answered", keyA, "", `{"cliSessionId":"` + appsConv + `","lastAssistantCliSessionId":"%s"}`, "prior"},
		{"left at", keyA, "", `{"cliSessionId":"` + appsConv + `","priorCliSessionLeftAt":{"%s":1}}`, "prior"},
		{"half written", keyA, appRec + ".json.tmp", `{"sessionId":"` + appRec + `","cliSessionId":"%s","cw`, "own"},
		{"over 10 MB", keyA, "", `{"cliSessionId":"%s","pad":"` + strings.Repeat("x", maxRecord+1) + `"}`, "own"},
		{"another account's list", keyX, "", `{"cliSessionId":"%s"}`, "own"},
		{"not a record", keyA, "", `[{"cliSessionId":"%s"}]`, "own"},
	}
	for _, c := range cases {
		for _, shows := range []bool{false, true} {
			t.Run(c.name+map[bool]string{false: "", true: ", baton shows another"}[shows], func(t *testing.T) {
				r := newRig(t)
				file := r.conversation(r.home, sid) // made in the app, or in a terminal
				if shows {
					r.apply(sid2, r.conversation(r.prof, sid2), keyB)
				}
				name := c.file
				if name == "" {
					name = appRec + ".json"
				}
				data := strings.ReplaceAll(strings.ReplaceAll(c.data, "%S", strings.ToUpper(sid)), "%s", sid)
				path := filepath.Join(r.placeDir(c.key), name)
				must(t, os.MkdirAll(filepath.Dir(path), 0o700))
				must(t, os.WriteFile(path, []byte(data), 0o600))
				res := r.refused(sid, appHas(c.detail))
				if len(res.Off) != 0 || len(res.Links) != 0 || !isFile(file) {
					t.Fatalf("result: %+v", res)
				}
				if !shows && (isFile(r.d.IndexPath) || isFile(r.d.LockPath)) {
					t.Fatal("a look with nothing of baton's wrote the index or its lock")
				}
				// Gone from the app: nothing in the way.
				must(t, os.Remove(path))
				if res := r.forget(sid, false); len(res.Blockers) != 0 || !isFile(file) {
					t.Fatalf("after the app let go: %+v", res)
				}
			})
		}
	}
}

// Every data folder of the desktop app counts, not only the one baton shows
// sessions in: the app reads it again when it goes back to it.
func TestForgetLooksInEveryDataFolder(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	second := filepath.Join(r.root, "Roaming", "Claude")
	must(t, os.MkdirAll(second, 0o700))
	must(t, os.WriteFile(filepath.Join(second, ".user-data-left.claude_test"), nil, 0o600))
	r.writeJSON(filepath.Join(second, sessionsDir, acctX, orgX, "local_d0000000-0000-4000-8000-0000000000d2.json"),
		map[string]any{"cliSessionId": appsConv, "priorCliSessionIds": []string{sid}})
	t.Setenv("BATON_DESKTOP_DATA", second)
	r.refused(sid, appHas("prior"))

	root := t.TempDir()
	la, ad := filepath.Join(root, "Local"), filepath.Join(root, "Roaming")
	for _, d := range []string{filepath.Join(la, "Claude-Data", "claude_pzs"), filepath.Join(la, "Claude-Data", "claude-code"),
		filepath.Join(ad, "Claude")} {
		must(t, os.MkdirAll(d, 0o700))
	}
	must(t, os.WriteFile(filepath.Join(la, "Claude-Data", "git-worktrees.json"), nil, 0o600))
	must(t, os.WriteFile(filepath.Join(ad, "Claude", ".user-data-left.claude_pzs"), nil, 0o600))
	got, unread := dataDirs(la, ad)
	want := []string{filepath.Join(la, "Claude-Data", "claude-code"), filepath.Join(la, "Claude-Data", "claude_pzs"),
		filepath.Join(la, "Claude-Data"), filepath.Join(ad, "Claude")}
	if !slices.Equal(got, want) || unread {
		t.Fatalf("data folders: %v (unread %v), want %v", got, unread, want)
	}
	if got, _ := dataDirs("", ""); len(got) != 0 {
		t.Fatalf("data folders with no profile folders: %v", got)
	}
	t.Setenv("BATON_DESKTOP_DATA", "")
	if dirs, _ := r.d.allData(); !slices.Equal(dirs, []string{r.data}) {
		t.Fatalf("a test binary looked for the real data folders: %v", dirs)
	}
	t.Setenv("BATON_DESKTOP_DATA", "none") // no desktop app: no folder by that name
	if dirs, _ := r.d.allData(); !slices.Equal(dirs, []string{r.data}) {
		t.Fatalf("with no desktop app: %v", dirs)
	}
}

// A record of baton's the desktop app took over is the app's: refused,
// whether the session is still on the conversation or went on from it.
func TestForgetRefusesWhatTheAppTookOver(t *testing.T) {
	t.Run("went on in the app", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.adopt(keyA, sid, 1)
		r.refused(sid, appHas("released"))
		if p := r.place(sid, keyA); p == nil || p.State != stateShown {
			t.Fatalf("what settle noted was saved by a refusal: %+v", p)
		}
	})
	t.Run("an earlier record", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		r.live(sid, "local_"+sid) // the app opens A's record: baton's session goes on in a copy
		copyFile := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, copyFile))
		ix := r.index()
		if th := ix.Thread(sid2); th == nil || len(th.Released) != 1 || th.Released[0].Cli != sid {
			t.Fatalf("setup: %+v", th)
		}
		r.refused(sid, appHas("released"))
	})
}

// What baton took off a list the running app still has in memory, or that
// the app wrote back, waits for the app; so does another session's record
// that still points at the conversation. A record of another session that
// names it only as an earlier conversation is in nobody's way.
func TestForgetWaitsForWhatTheAppMayWriteBack(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		r.running = true
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		r.apply(sid2, next)
		r.apply(sid2, next, keyA) // ticked again before the app let go of local_<sid>
		if ix := r.index(); !ix.loaded(sid) {
			t.Fatalf("setup: %+v", r.index().Gone)
		}
		r.refused(sid, isBusy)
	})
	t.Run("written back", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyB)
		rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
		before, _ := os.ReadFile(rec)
		r.apply(sid, file)
		must(t, os.WriteFile(rec, before, 0o600)) // the app ran unnoticed
		r.refused(sid, isBusy)
		must(t, r.d.Reconcile())
		if res := r.forget(sid, false); len(res.Blockers) != 0 || isFile(rec) {
			t.Fatalf("once taken off again: %+v", res)
		}
	})
	t.Run("another session's record waits to move on", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.running = true // signed in to A
		r.apply(sid, file, keyA, keyB)
		next := r.conversation(r.prof, sid2)
		must(t, r.d.Follow(sid, sid2, next))
		if r.record(keyA, "local_"+sid)["cliSessionId"] != sid || r.record(keyB, "local_"+sid)["cliSessionId"] != sid2 {
			t.Fatal("setup")
		}
		r.refused(sid, isWaiting)
		r.running = false
		must(t, r.d.Reconcile())
		res := r.forget(sid, false)
		if len(res.Blockers) != 0 || len(res.Off) != 0 {
			t.Fatalf("once pointed on: %+v", res)
		}
		for _, k := range []string{keyA, keyB} {
			m := r.record(k, "local_"+sid)
			if prior, _ := m["priorCliSessionIds"].([]any); m["cliSessionId"] != sid2 || len(prior) != 1 || prior[0] != sid {
				t.Fatalf("the session's record in %s was touched: %v", k, m)
			}
		}
	})
}

// A record the user deleted in the desktop app is gone from there: the
// conversation can go, the place stays deleted, and what the delete left
// (deleted_<id>, <id>.desktop-released.json) stays as the app left it.
func TestForgetAfterADeleteInTheApp(t *testing.T) {
	t.Run("baton's record", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA, keyB)
		tomb, marker := r.deleteInApp(keyB, "local_"+sid, sid, file, 1_800_000_005_000)
		left := map[string]string{}
		for _, p := range []string{tomb, marker} {
			b, _ := os.ReadFile(p)
			left[p] = string(b)
		}
		res := r.forget(sid, false)
		if len(res.Blockers) != 0 || !slices.Equal(res.Off, []string{"a, a2"}) {
			t.Fatalf("result: %+v", res)
		}
		if p := r.place(sid, keyB); p == nil || p.State != stateDeleted || r.place(sid, keyA) != nil || r.record(keyA, "local_"+sid) != nil {
			t.Fatalf("places: %+v", r.index().Threads)
		}
		for p, b := range left {
			if got, err := os.ReadFile(p); err != nil || string(got) != b {
				t.Fatalf("%s changed or went", p)
			}
		}
	})
	t.Run("the app's own", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.home, sid)
		must(t, os.WriteFile(filepath.Join(r.placeDir(keyA), "deleted_"+sid), []byte("1800000005000"), 0o600))
		must(t, os.WriteFile(filepath.Join(filepath.Dir(file), sid+".desktop-released.json"), []byte(`{"v":1,"reason":"delete"}`), 0o600))
		before := r.files()
		if res := r.forget(sid, false); len(res.Blockers) != 0 || len(res.Off) != 0 {
			t.Fatalf("result: %+v", res)
		}
		r.unchanged(before, "nothing of baton's")
	})
}

// The session moved on from the conversation (/clear): its records name it
// only as an earlier conversation, which is in nobody's way, and they stay.
func TestForgetAnEarlierConversationOfASession(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	must(t, r.d.Reconcile())
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	before, _ := os.ReadFile(rec)
	if !strings.Contains(string(before), sid) {
		t.Fatal("setup")
	}
	if res := r.forget(sid, false); len(res.Blockers) != 0 || len(res.Off) != 0 {
		t.Fatalf("result: %+v", res)
	}
	if after, _ := os.ReadFile(rec); string(after) != string(before) {
		t.Fatal("the session's record was touched")
	}
}

// A second name something still has open stays (the delete takes it with
// the file's other names): Forget says so.
func TestForgetKeepsASecondNameInUse(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	h := r.second(file, sid)
	r.liveIn(r.prof, sid) // a Claude Code in the profile may have it open by that name
	res := r.forget(sid, false)
	if len(res.Blockers) != 0 || !slices.Equal(res.Links, []string{h}) || !sameFile(h, file) {
		t.Fatalf("result: %+v", res)
	}
	if r.record(keyA, "local_"+sid) != nil {
		t.Fatal("baton's record stayed")
	}
}

// A record baton has not written yet goes from the index alone; a file of
// its name is the app's.
func TestForgetARecordNotWrittenYet(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	th := &Thread{Session: sid, Transcript: file, Places: map[string]*Place{keyB: {Key: keyB, Dir: r.placeDir(keyB),
		Record: "local_" + sid, Cli: sid, State: stateShown, Label: "b"}}}
	must(t, writeIndex(r.d.IndexPath, Index{Schema: 1, Threads: []*Thread{th}}))
	theirs := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	r.writeJSON(theirs, map[string]any{"cliSessionId": sid})
	if res := r.refused(sid, appHas("own")); !slices.Equal(res.Off, []string{"b"}) {
		t.Fatalf("result: %+v", res)
	}
	must(t, os.Remove(theirs))
	if res := r.forget(sid, false); len(res.Blockers) != 0 || !slices.Equal(res.Off, []string{"b"}) {
		t.Fatalf("result: %+v", res)
	}
	if ix := r.index(); len(ix.Threads) != 0 || len(ix.Gone) != 0 {
		t.Fatalf("index: %+v", ix)
	}
}

// A list or record that cannot be read right now may name the conversation.
func TestForgetCannotCheck(t *testing.T) {
	for _, what := range []string{"record", "list"} {
		t.Run(what, func(t *testing.T) {
			r := newRig(t)
			file := r.conversation(r.prof, sid)
			r.apply(sid, file, keyA)
			own := r.appRecord(keyB, "local_d0000000-0000-4000-8000-0000000000d1", appsConv)
			held := own
			if what == "list" {
				held = r.placeDir(keyB)
			}
			release := hold(t, held)
			r.refused(sid, isUnread)
			release()
			if res := r.forget(sid, false); len(res.Blockers) != 0 || r.record(keyA, "local_"+sid) != nil {
				t.Fatalf("once it could be read: %+v", res)
			}
		})
	}
}

// Only a conversation id is asked about.
func TestForgetBadID(t *testing.T) {
	r := newRig(t)
	for _, id := range []string{"", "..", `x\y`, sid + "x"} {
		if _, err := r.d.Forget(id, true); err == nil || err.(*Error).Code != "no_file" {
			t.Fatalf("%q: %v", id, err)
		}
	}
	if isFile(r.d.IndexPath) || isFile(r.d.LockPath) {
		t.Fatal("written")
	}
}
