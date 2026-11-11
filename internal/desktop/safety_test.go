package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The conversation's only name is never removed: not after Claude Code's
// cleanup took the profile's name and the session went on from the second
// name, nor when the session ran from the second name before that, nor when
// the user's own folder is spelled another way (8.3), nor when an older
// index names one file twice.
func TestOnlyNameNeverRemoved(t *testing.T) {
	second := func(r *rig, file string) string {
		return filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	}
	t.Run("swept, then went on from the second name", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		s := second(r, file)
		must(t, os.Remove(file)) // Claude Code's cleanup in the profile
		must(t, r.d.Touch(sid, s))
		if l := r.index().Threads[0].Links; len(l) != 0 {
			t.Fatalf("the only name is still listed as a second name: %+v", l)
		}
		r.apply(sid, s)
		if !isFile(s) {
			t.Fatal("the only name of the conversation was removed")
		}
	})
	t.Run("ran from the second name, then swept", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		s := second(r, file)
		must(t, r.d.Touch(sid, s)) // baton run -- --resume <the second name>
		th := r.index().Threads[0]
		if !samePath(th.Transcript, file) || !samePath(th.Links[sid].File, file) || !samePath(th.Links[sid].Path, s) {
			t.Fatalf("baton did not keep to its own name: %+v", th)
		}
		must(t, os.Remove(file))
		r.apply(sid, s)
		must(t, r.d.Reconcile())
		if !isFile(s) {
			t.Fatal("the only name of the conversation was removed")
		}
	})
	t.Run("the user's folder spelled in 8.3 form", func(t *testing.T) {
		r := newRig(t)
		long := filepath.Join(r.root, "a long folder name for the user")
		must(t, os.MkdirAll(filepath.Join(long, ".claude"), 0o700))
		short := shortPath(long)
		if strings.EqualFold(short, long) {
			t.Skip("no 8.3 names on this volume")
		}
		r.d.UserHome = filepath.Join(long, ".claude")
		file := r.conversation(filepath.Join(short, ".claude"), sid)
		if !r.d.inUserHome(file) {
			t.Fatalf("%s is in %s", file, r.d.UserHome)
		}
		r.apply(sid, file, keyA)
		if l := r.index().Threads[0].Links; len(l) != 0 {
			t.Fatalf("a file in the user's own folder got a second name: %+v", l)
		}
		r.apply(sid, file)
		if !isFile(file) {
			t.Fatal("the only name of the conversation was removed")
		}
	})
	t.Run("an index that names one file twice", func(t *testing.T) {
		r := newRig(t)
		file := r.conversation(r.prof, sid)
		r.apply(sid, file, keyA)
		s := second(r, file)
		ix := r.index()
		ix.Threads[0].Links[sid] = Link{Path: s, File: strings.ToUpper(s)}
		must(t, os.Remove(file))
		ix.Threads[0].Transcript = s
		must(t, writeIndex(r.d.IndexPath, ix))
		r.apply(sid, s)
		must(t, r.d.Reconcile())
		if !isFile(s) {
			t.Fatal("the only name of the conversation was removed")
		}
	})
}

func shortPath(p string) string {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(u, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}

func TestLinkCount(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	must(t, os.WriteFile(a, []byte("x"), 0o600))
	if n := linkCount(a); n != 1 {
		t.Fatalf("one name: %d", n)
	}
	must(t, link(a, b))
	if n := linkCount(b); n != 2 {
		t.Fatalf("two names: %d", n)
	}
	if n := linkCount(filepath.Join(dir, "none")); n != 0 {
		t.Fatalf("no file: %d", n)
	}
}

// The desktop app is on another organization of its account than the one
// the user's own Claude Code names: while it runs, every list of that
// account is left to it. A record it writes back after an untick is still
// baton's, never shown as made in the app, and goes once the app is closed.
func TestRunningAppAnyOrganization(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	orgA2 := "a0000000-0000-4000-8000-0000000000a2"
	r.d.Accounts[2].OrgID = orgA2 // a2: the same Claude account, a Team organization
	keyA2 := acctA + "/" + orgA2
	must(t, os.MkdirAll(r.placeDir(keyA2), 0o700))
	r.writeJSON(filepath.Join(filepath.Dir(r.home), ".claude.json"), map[string]any{"oauthAccount": map[string]any{
		"accountUuid": acctA, "organizationUuid": orgA, "emailAddress": "a@example.com"}})
	r.running = true
	if v := r.view(sid, file); !rowOf(v, keyA).Active || rowOf(v, keyA2).Active {
		t.Fatalf("the window names the organization the user's Claude Code is on: %+v", v.Rows)
	}
	res := r.apply(sid, file, keyA2)
	if res.ActiveAdded || !res.OtherAdded {
		t.Fatalf("result: %+v", res)
	}
	rec := filepath.Join(r.placeDir(keyA2), "local_"+sid+".json")
	before, _ := os.ReadFile(rec)
	res = r.apply(sid, file)
	if !res.ActiveRemoved || isFile(rec) {
		t.Fatalf("untick while the app runs on that account: %+v", res)
	}
	must(t, os.WriteFile(rec, before, 0o600)) // the app writes it back from memory as it quits
	if row := rowOf(r.view(sid, file), keyA2); row.Checked || row.Locked || row.Why != "" {
		t.Fatalf("baton's record written back is shown as the app's: %+v", row)
	}
	must(t, r.d.Reconcile())
	if !isFile(rec) {
		t.Fatal("changed in the running app's account")
	}
	r.running = false
	must(t, r.d.Reconcile())
	if isFile(rec) {
		t.Fatal("the record written back stayed once the app was closed")
	}
}

// Running-app detection missed the app (or it had another list open): the
// record it writes back later is still recognized as baton's, taken off
// again, and ticking the session again takes that file back rather than
// making a second record.
func TestWrittenBackLater(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	before, _ := os.ReadFile(rec)
	r.apply(sid, file)
	if g := r.index().Gone; len(g) != 1 || g[0].Record != "local_"+sid || !g[0].knows(sid) {
		t.Fatalf("gone: %+v", g)
	}
	must(t, os.WriteFile(rec, before, 0o600))
	if row := rowOf(r.view(sid, file), keyB); row.Checked || row.Locked {
		t.Fatalf("row: %+v", row)
	}
	r.apply(sid, file, keyB) // ticked again before baton took it off
	if recs := listRecords(r.placeDir(keyB)); len(recs) != 1 {
		t.Fatalf("a second record next to the one written back: %v", recs)
	}
	if ix := r.index(); len(ix.Gone) != 0 || ix.Threads[0].Places[keyB].Record != "local_"+sid {
		t.Fatalf("index: %+v", ix)
	}
	r.apply(sid, file)
	must(t, os.WriteFile(rec, before, 0o600))
	must(t, r.d.Reconcile())
	if isFile(rec) {
		t.Fatal("a record of baton's written back was not taken off again")
	}

	// One the app went on with (a conversation baton never had) is the app's.
	m := map[string]any{}
	must(t, json.Unmarshal(before, &m))
	m["cliSessionId"] = "f0000000-0000-4000-8000-00000000000f"
	b, _ := json.Marshal(m)
	must(t, os.WriteFile(rec, b, 0o600))
	must(t, r.d.Reconcile())
	if got, _ := os.ReadFile(rec); string(got) != string(b) || len(r.index().Gone) != 0 {
		t.Fatal("a record the app went on with was touched or is still counted as baton's")
	}

	// Forgotten after goneFor.
	r.apply(sid, file, keyA)
	r.apply(sid, file)
	r.d.Now = func() time.Time { return time.UnixMilli(1_800_000_000_000).Add(goneFor + time.Hour) }
	must(t, r.d.Reconcile())
	if len(r.index().Gone) != 0 {
		t.Fatalf("kept after %v: %+v", goneFor, r.index().Gone)
	}
}

// A record that could not be pointed at the new conversation keeps the old
// one's second name until it is.
func TestRepointFailureKeepsSecondName(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	old := filepath.Join(r.home, "projects", filepath.Base(filepath.Dir(file)), sid+".jsonl")
	r.view(sid, file) // the record is read (and kept) before it is locked
	h := lockFile(t, rec)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	if p := r.index().Threads[0].Places[keyB]; p.Cli != sid || !sameFile(old, file) {
		t.Fatalf("the index says it was repointed, or the old second name went: %+v", p)
	}
	windows.CloseHandle(h)
	must(t, r.d.Reconcile())
	if p := r.index().Threads[0].Places[keyB]; p.Cli != sid2 || isFile(old) {
		t.Fatalf("not repointed once it could be: %+v", p)
	}
}

// lockFile opens path so nothing else can read or replace it.
func lockFile(t *testing.T, path string) windows.Handle {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	must(t, err)
	t.Cleanup(func() { windows.CloseHandle(h) })
	return h
}

// The index names a record before it is written: a stop right after the
// write leaves the record known as baton's, not one more of the app's.
func TestIndexSavedBeforeRecord(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyA)
	r.live(sid, "local_"+sid) // the app opens it: baton's session goes on in a copy
	next := filepath.Join(filepath.Dir(file), sid2+".jsonl")
	must(t, r.d.Follow(sid, sid2, next))
	r.conversation(r.prof, sid2)
	func() {
		defer func() { _ = recover() }()
		_ = r.d.update(func(ix *Index) error {
			r.d.reconcile(ix, ix.Thread(sid2), r.d.look(ix))
			panic("stopped before the index was saved at the end")
		})
	}()
	copyRec := filepath.Join(r.placeDir(keyA), "local_"+sid2+".json")
	if !isFile(copyRec) {
		t.Fatal("the copy's record was not written")
	}
	if p := r.index().Threads[0].Places[keyA]; p == nil || p.Written == 0 || p.Record != "local_"+sid2 {
		t.Fatalf("the index on disk does not name the record: %+v", p)
	}
	must(t, r.d.Reconcile())
	if recs := listRecords(r.placeDir(keyA)); len(recs) != 2 {
		t.Fatalf("records: %v", recs)
	}
	if row := rowOf(r.view(sid2, next), keyA); !row.Checked || !row.Copy || row.Locked {
		t.Fatalf("row: %+v", row)
	}
}

// A list baton made the folder of counts as unused by the desktop app until
// the app has something of its own there.
func TestFolderBatonMade(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	if b := rowOf(r.view(sid, file), keyB); b.Used {
		t.Fatalf("used once baton made its folder: %+v", b)
	}
	r.apply(sid, file)
	if b := rowOf(r.view(sid, file), keyB); b.Used {
		t.Fatalf("used after unticking: %+v", b)
	}
	r.writeJSON(filepath.Join(r.placeDir(keyB), "local_d0000000-0000-4000-8000-0000000000d0.json"), map[string]any{
		"sessionId": "local_d0000000-0000-4000-8000-0000000000d0", "cliSessionId": sid2, "cwd": r.proj})
	if b := rowOf(r.view(sid, file), keyB); !b.Used {
		t.Fatalf("not used once the app has a record of its own there: %+v", b)
	}
	// A folder baton made does not make the app's organization unclear.
	r.writeJSON(filepath.Join(r.data, "config.json"), map[string]any{"lastKnownAccountUuid": acctX})
	must(t, os.MkdirAll(r.placeDir(keyX), 0o700))
	r.d.Accounts = append(r.d.Accounts, r.d.Accounts[0])
	r.d.Accounts[len(r.d.Accounts)-1].ID, r.d.Accounts[len(r.d.Accounts)-1].AccountUUID = "x2", acctX
	r.d.Accounts[len(r.d.Accounts)-1].OrgID = orgA
	r.apply(sid, file, acctX+"/"+orgA)
	ix := r.index()
	if org, _ := r.d.appAccount(&ix, acctX); org != orgX {
		t.Fatalf("the app's organization: %q", org)
	}
}

// What the window says after Apply: the account the app is signed in to
// shows when it opens, others when the user switches to them.
func TestApplyResultForTheMessage(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	if res := r.apply(sid, file, keyB); res.ActiveAdded || !res.OtherAdded || res.AppRunning {
		t.Fatalf("another account, app closed: %+v", res)
	}
	if res := r.apply(sid, file, keyA, keyB); !res.ActiveAdded || res.OtherAdded {
		t.Fatalf("the app's account, app closed: %+v", res)
	}
	r.running = true
	if res := r.apply(sid, file, keyA); res.ActiveRemoved || len(res.Removed) != 1 || !res.AppRunning {
		t.Fatalf("another account taken off while the app runs: %+v", res)
	}
	if res := r.apply(sid, file); !res.ActiveRemoved {
		t.Fatalf("the app's account taken off while it runs: %+v", res)
	}
}

// What waits for the desktop app to close, so the window looks for that
// every second.
func TestWaiting(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.running = true
	r.apply(sid, file, keyA, keyB)
	if r.index().Waiting(r.data) {
		t.Fatal("waiting with nothing to do")
	}
	r.apply(sid, file, keyB) // the app's account: waits
	if !r.index().Waiting(r.data) {
		t.Fatal("an untick in the running app's account does not wait")
	}
	r.running = false
	must(t, r.d.Reconcile())
	if r.index().Waiting(r.data) {
		t.Fatal("still waiting once done")
	}
	// After /clear the record in the app's account waits to be pointed on.
	r.running = true
	r.apply(sid, file, keyA, keyB)
	next := r.conversation(r.prof, sid2)
	must(t, r.d.Follow(sid, sid2, next))
	if !r.index().Waiting(r.data) {
		t.Fatal("a repoint in the running app's account does not wait")
	}
	r.running = false
	must(t, r.d.Reconcile())
	if r.index().Waiting(r.data) || r.record(keyA, "local_"+sid)["cliSessionId"] != sid2 {
		t.Fatal("not repointed once the app closed")
	}
}

// Ways of signing in that have no list in the desktop app say which.
func TestAccountPlaceReasons(t *testing.T) {
	r := newRig(t)
	for method, want := range map[string]string{"api_key": "api_key", "apiKey": "api_key", "oauth_token": "token",
		"third_party": "gateway", "bedrock": "cloud", "vertex": "cloud", "foundry": "cloud"} {
		a := r.d.Accounts[0]
		a.AuthMethod = method
		if _, why, _ := accountPlace(a); why != want {
			t.Errorf("%s: %s, want %s", method, why, want)
		}
	}
}

// Records that are not JSON objects are no records, and are never changed.
func TestNotARecord(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	rec := filepath.Join(r.placeDir(keyB), "local_"+sid+".json")
	must(t, os.WriteFile(rec, []byte("null"), 0o600))
	if _, ok := readHead(rec); ok {
		t.Fatal("null read as a record")
	}
	if err := rewrite(rec, func(m map[string]any) { m["x"] = 1 }); err == nil {
		t.Fatal("rewrote null")
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"type":"assistant","timestamp":"2026-11-03T09:00:00.000Z","message":{"role":"assistant","content":"more"}}` + "\n")
	f.Close()
	must(t, r.d.Reconcile())
	if b, _ := os.ReadFile(rec); string(b) != "null" {
		t.Fatalf("changed: %s", b)
	}
}

// baton's own temporary files left in a list go once they are an hour old;
// nothing else there does.
func TestSweepTemp(t *testing.T) {
	r := newRig(t)
	file := r.conversation(r.prof, sid)
	r.apply(sid, file, keyB)
	dir := r.placeDir(keyB)
	oldTmp, newTmp, other := filepath.Join(dir, ".baton-1.tmp"), filepath.Join(dir, ".baton-2.tmp"), filepath.Join(dir, "local_x.json.tmp")
	for _, p := range []string{oldTmp, newTmp, other} {
		must(t, os.WriteFile(p, []byte("{"), 0o600))
	}
	hourAgo := time.Now().Add(-2 * time.Hour)
	must(t, os.Chtimes(oldTmp, hourAgo, hourAgo))
	must(t, os.Chtimes(other, hourAgo, hourAgo))
	must(t, r.d.Reconcile())
	if isFile(oldTmp) || !isFile(newTmp) || !isFile(other) {
		t.Fatal("swept the wrong files")
	}
	if recs := listRecords(dir); len(recs) != 1 {
		t.Fatalf("records: %v", recs)
	}
}
