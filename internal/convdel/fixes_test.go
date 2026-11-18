package convdel

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"baton/internal/claude"
	"baton/internal/desktop"
	"baton/internal/recycle"
	"baton/internal/run"
	"baton/internal/share"
)

// A name of the conversation's file the scan can't see (through a link or
// junction, or in a folder that can't be read) refuses the delete, with
// nothing moved: the lists and Claude Code's resume find the file through
// the link, so moving only what was seen would leave it behind without its
// checkpoints and runs, and say it went. A linked project folder that holds
// nothing of it does not count.
func TestHiddenNames(t *testing.T) {
	t.Run("the projects folder is a junction", func(t *testing.T) {
		w := newWorld(t)
		moved := filepath.Join(w.root, "D", "projects")
		w.conv(filepath.Dir(moved), sid) // D\projects\C--Proj\<sid>.jsonl
		if !junction(t, filepath.Join(w.home, "projects"), moved) {
			t.Skip("no junctions here")
		}
		w.write(filepath.Join(w.home, "file-history", sid, "v1"), "old")
		w.write(filepath.Join(w.home, "session-env", sid, "hook-1.sh"), "set x=1")
		w.write(filepath.Join(w.temp, "claude", "C--Proj", sid, "notes.txt"), "notes")
		if p := claude.FindTranscript([]string{w.home}, sid); p == "" {
			t.Fatal("the lists would not find it either")
		}
		w.refused(w.req(sid, "claude-code"), Blocker{Code: CodeLinkedFolder})
	})
	t.Run("a project folder is a junction", func(t *testing.T) {
		w := newWorld(t)
		moved := filepath.Join(w.root, "D2", w.slug)
		w.write(filepath.Join(moved, sid+".jsonl"), `{"type":"user","sessionId":"`+sid+`"}`+"\n")
		if !junction(t, filepath.Join(w.profA, "projects", w.slug), moved) {
			t.Skip("no junctions here")
		}
		w.write(filepath.Join(w.profA, "file-history", sid, "v1"), "old")
		w.journal("r1", run.Journal{SessionID: sid, State: run.StateDone, SupervisorPID: 4001})
		w.refused(w.req(sid, "a"), Blocker{Code: CodeLinkedFolder})
	})
	t.Run("an unrelated project folder is a junction", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		elsewhere := filepath.Join(w.root, "D3", "Other")
		w.write(filepath.Join(elsewhere, other+".jsonl"), "{}\n")
		if !junction(t, filepath.Join(w.profB, "projects", "Other"), elsewhere) {
			t.Skip("no junctions here")
		}
		if _, err := Delete(w.req(sid, "a")); err != nil {
			t.Fatal(err)
		}
		if exists(a) || !exists(filepath.Join(elsewhere, other+".jsonl")) {
			t.Fatal("the conversation stayed, or the other went")
		}
	})
	// No name of the file is seen, and nothing in the profile's root says
	// baton made it: the plan has no name, and made_in stays "outside",
	// which then says nothing of where it was made (the window and the CLI
	// do not say it was made outside baton for a plan with no name).
	t.Run("a profile's projects folder can't be listed", func(t *testing.T) {
		w := newWorld(t)
		w.conv(w.profA, sid)
		projects := filepath.Join(w.profA, "projects")
		if out, err := exec.Command("icacls", projects, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
			t.Skipf("icacls: %v %s", err, out)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", projects, "/remove:d", "*S-1-1-0").Run() })
		if _, err := os.ReadDir(projects); err == nil {
			t.Skip("the folder can still be listed")
		}
		w.refused(w.req(sid, "a"), Blocker{Code: CodeLinkedFolder})
		p, err := Look(w.req(sid, "a"))
		if err != nil || p.Title != "" || p.Cwd != "" || p.Source != "" || p.MadeIn != "outside" {
			t.Fatalf("title %q, cwd %q, source %q, made in %q: %v", p.Title, p.Cwd, p.Source, p.MadeIn, err)
		}
	})
	t.Run("a project folder that can't be read", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		history := w.write(filepath.Join(w.profA, "file-history", sid, "v1"), "old")
		folder := filepath.Dir(a)
		// Nobody may list it (the file in it can still be opened by name).
		if out, err := exec.Command("icacls", folder, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
			t.Skipf("icacls: %v %s", err, out)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", folder, "/remove:d", "*S-1-1-0").Run() })
		if _, err := os.ReadDir(folder); err == nil {
			t.Skip("the folder can still be listed")
		}
		before, _ := os.ReadFile(a)
		w.refused(w.req(sid, "a"), Blocker{Code: CodeLinkedFolder})
		if got, err := os.ReadFile(a); err != nil || string(got) != string(before) || !exists(history) {
			t.Fatalf("the conversation changed: %v, file-history there %v", err, exists(history))
		}
	})
}

// A conversation whose file the scan sees only through a link is refused,
// and the plan still says which conversation it is (its title, folder and
// where it was made), not what an earlier delete left behind: the lists show
// it all the same.
func TestLinkedFolderSaysWhichConversation(t *testing.T) {
	for _, c := range []struct{ name, where, madeIn string }{
		{"the user's own projects folder", "claude-code", "outside"},
		{"a project folder in a profile", "a", "baton"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			file := w.conv(filepath.Join(w.root, "D"), sid) // D\projects\C--Proj\<sid>.jsonl
			link, target := filepath.Join(w.home, "projects"), filepath.Dir(filepath.Dir(file))
			if c.where == "a" {
				link, target = filepath.Join(w.profA, "projects", w.slug), filepath.Dir(file)
			}
			if !junction(t, link, target) {
				t.Skip("no junctions here")
			}
			p, err := Look(w.req(sid, c.where))
			if err != nil || p.OK || len(p.Blockers) == 0 || p.Blockers[0].Code != CodeLinkedFolder {
				t.Fatalf("%+v %v", p, err)
			}
			if p.Title != "fix the login bug" || p.Cwd != w.proj || p.MadeIn != c.madeIn {
				t.Fatalf("title %q, cwd %q, made in %q", p.Title, p.Cwd, p.MadeIn)
			}
		})
	}
}

// After a delete that stopped half way, the name it already put in the
// Recycle Bin is still a hard link of the file; that is no name outside
// Claude Code's folders, and the retry finishes. So is a name the user put
// back from the bin while the other stays there.
func TestANameInTheBinIsNoOtherName(t *testing.T) {
	t.Run("a retry after part went", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		h := w.link(a) // a second name no desktop thread tracks: Forget leaves it
		history := filepath.Dir(w.write(filepath.Join(w.profA, "file-history", sid, "v1"), "old"))
		r := w.req(sid, "a")
		r.Bin = &failBin{Bin: r.Bin, at: 1}
		_, err := Delete(r)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodePartly || e.Moved != 1 || e.Left != 2 || exists(h) || !exists(a) {
			t.Fatalf("first delete: %v (%+v)", err, e)
		}
		p, err := Look(w.req(sid, "a"))
		if err != nil || !p.OK || !slices.Equal(p.Paths(), []string{a, history}) {
			t.Fatalf("again: %+v %q %v", p.Blockers, p.Paths(), err)
		}
		if _, err := Delete(w.req(sid, "a")); err != nil {
			t.Fatal(err)
		}
		if got := w.binned(); !slices.Equal(got, []string{h, a, history}) || exists(a) || exists(history) {
			t.Fatalf("binned %q", got)
		}
	})
	t.Run("one name put back from the bin", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		w.link(a)
		if _, err := Delete(w.req(sid, "a")); err != nil {
			t.Fatal(err)
		}
		m, err := recycle.ReadManifest(w.bin)
		must(t, err)
		i := slices.IndexFunc(m, func(e recycle.Entry) bool { return e.From == a })
		if i < 0 {
			t.Fatalf("manifest: %+v", m)
		}
		must(t, os.Rename(m[i].To, a))
		p, err := Look(w.req(sid, "a"))
		if err != nil || !p.OK || !slices.Equal(p.Paths(), []string{a}) {
			t.Fatalf("put back: %+v %q %v", p.Blockers, p.Paths(), err)
		}
		if _, err := Delete(w.req(sid, "a")); err != nil || exists(a) {
			t.Fatalf("delete again: %v", err)
		}
	})
	t.Run("a name outside the bin still counts", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		h := w.link(a)
		must(t, os.MkdirAll(filepath.Join(w.root, "elsewhere"), 0o700))
		must(t, os.Link(a, filepath.Join(w.root, "elsewhere", "saved.jsonl")))
		r := w.req(sid, "a")
		r.Bin = &failBin{Bin: r.Bin, at: 1}
		if _, err := Delete(r); codeOf(err) != CodeOtherName {
			t.Fatalf("delete: %v", err)
		}
		if !exists(h) || len(w.binned()) != 0 {
			t.Fatal("something moved")
		}
	})
}

// A baton session of the conversation that ended while its baton lives on
// (its window shows the last of it, and holds the run's log open) refuses
// the delete before anything moves. Once that window is closed, the run goes
// with the conversation.
func TestEndedSessionWindowStillOpen(t *testing.T) {
	w := newWorld(t)
	w.conv(w.profA, sid)
	ended := w.journal("r1", run.Journal{SessionID: sid, State: run.StateDone, SupervisorPID: 4001})
	log, err := os.OpenFile(filepath.Join(ended, "baton.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	must(t, err)
	_, _ = log.WriteString("the last of it\n")
	w.alive[4001] = true
	w.refused(w.req(sid, "a"), Blocker{Code: CodeSessionWindow, RunID: "r1"})
	// Also once its conversation went: the run is what is left of it.
	other := w.journal("r2", run.Journal{SessionID: copyID, State: run.StateDone, SupervisorPID: 4002})
	w.alive[4002] = true
	w.refused(w.req(copyID, "a"), Blocker{Code: CodeSessionWindow, RunID: "r2"})
	w.alive[4002] = false
	if res, err := Delete(w.req(copyID, "a")); err != nil || res.Moved != 1 || exists(other) {
		t.Fatalf("what was left: %+v %v", res, err)
	}

	log.Close()
	w.alive[4001] = false
	res, err := Delete(w.req(sid, "a"))
	if err != nil || res.Moved != 2 || exists(ended) {
		t.Fatalf("after the window closed: %+v %v", res, err)
	}
}

// A baton session that waits for an account before it resumes the
// conversation (its journal names it only by Opens) refuses the delete.
func TestARunWaitingToOpenIt(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.journal("r9", run.Journal{State: run.StateWaiting, SupervisorPID: 4242, Opens: strings.ToUpper(a)})
	w.alive[4242] = true
	w.refused(w.req(sid, "a"), Blocker{Code: CodeInBaton, RunID: "r9"})
	// By its second name too: the same file.
	w.link(a)
	w.refused(w.req(sid, "claude-code"), Blocker{Code: CodeInBaton, RunID: "r9"})
}

// A tick in the desktop app between the delete taking the conversation off
// it and the move (the CLI's, outside the window's gate) is refused: the
// delete finishes with nothing left, and no record or second name outlives
// it.
func TestATickWhileItGoes(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.show(a)
	h := filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
	r := w.req(sid, "a")
	var tick error
	r.Office = func(string) {
		_, tick = w.desk.Apply(desktop.Conversation{ID: sid, Transcript: a}, []string{keyA})
	}
	res, err := Delete(r)
	var de *desktop.Error
	if !errors.As(tick, &de) || de.Code != "deleting" {
		t.Fatalf("the tick: %v", tick)
	}
	if err != nil || res.Warning != "" {
		t.Fatalf("delete: %+v %v", res, err)
	}
	if exists(a) || exists(h) || len(w.records()) != 0 {
		t.Fatalf("left: profile name %v, second name %v, records %q", exists(a), exists(h), w.records())
	}
	if _, err := Look(w.req(sid, "a")); codeOf(err) != CodeGone {
		t.Fatalf("look afterwards: %v", err)
	}
}

// What the delete was to move went elsewhere meanwhile (an account removed
// from the CLI moves the profile's conversations into the user's own
// folder): the delete says so, instead of saying it went to the bin.
func TestMovedMeanwhile(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.write(filepath.Join(w.profA, "projects", w.slug, sid, "subagents", "agent-1.jsonl"), "{}")
	w.write(filepath.Join(w.profA, "file-history", sid, "v1"), "old")
	r := w.req(sid, "a")
	r.Office = func(string) {
		if n, err := share.KeepConversations(w.profA, w.home, "a"); err != nil || n != 1 {
			t.Errorf("remove moved %d: %v", n, err)
		}
	}
	res, err := Delete(r)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeChanged || e.Status() != http.StatusConflict || res.Moved != 0 {
		t.Fatalf("delete: %+v %v", res, err)
	}
	moved := filepath.Join(w.home, "projects", w.slug, sid+".jsonl")
	if exists(a) || !exists(moved) || len(w.binned()) != 0 {
		t.Fatalf("profile name %v, in the user's folder %v, binned %q", exists(a), exists(moved), w.binned())
	}
	if v := w.hidden(); !slices.Contains(v, sid) {
		t.Fatalf("unhidden though it is still there: %q", v)
	}
	// Deleted where it is now, it goes.
	if _, err := Delete(w.req(sid, "claude-code")); err != nil || exists(moved) {
		t.Fatalf("again: %v", err)
	}
}

// Temporary folders alone are no conversation of baton's: Claude Code keeps
// them by id for any config folder, also one baton never looks in where the
// conversation may be open now. They are left, unless baton's own delete of
// it stopped half way, which then finishes.
func TestOnlyTemporaryFoldersLeft(t *testing.T) {
	t.Run("a conversation elsewhere", func(t *testing.T) {
		w := newWorld(t)
		elsewhere := filepath.Join(w.root, "other-config")
		w.conv(elsewhere, sid)
		w.write(filepath.Join(w.temp, "claude", "C--Proj", sid, "tasks", "b1.output"), "running")
		before := w.files()
		if _, err := Look(w.req(sid, "")); codeOf(err) != CodeGone {
			t.Fatalf("look: %v", err)
		}
		if _, err := Delete(w.req(sid, "")); codeOf(err) != CodeGone {
			t.Fatalf("delete: %v", err)
		}
		w.unchanged(before, "gone")
	})
	t.Run("what a stopped delete left", func(t *testing.T) {
		w := newWorld(t)
		a := w.conv(w.profA, sid)
		temp := filepath.Dir(w.write(filepath.Join(w.temp, "claude", "C--Proj", sid, "notes.txt"), "notes"))
		r := w.req(sid, "a")
		r.Bin = &failBin{Bin: r.Bin, at: 1}
		if _, err := Delete(r); codeOf(err) != CodePartly || exists(a) || !run.Left(w.paths.Root, sid) {
			t.Fatalf("first delete: %v", err)
		}
		p, err := Look(w.req(sid, "a"))
		if err != nil || !p.OK || !slices.Equal(p.Paths(), []string{temp}) {
			t.Fatalf("again: %+v %v", p, err)
		}
		if _, err := Delete(w.req(sid, "a")); err != nil || exists(temp) || run.Left(w.paths.Root, sid) {
			t.Fatalf("finished: %v, note left %v", err, run.Left(w.paths.Root, sid))
		}
	})
}

// A Recycle Bin failure baton has no words for comes back as recycle_failed,
// naming no file (the bin's own text stays in Err), and still says the
// conversation is off the desktop app; so does one the plan meets.
func TestAnUnknownBinFailure(t *testing.T) {
	w := newWorld(t)
	a := w.conv(w.profA, sid)
	w.show(a)
	r := w.req(sid, "a")
	r.Bin = &oddBin{Bin: r.Bin, err: &recycle.Error{Path: a, Drive: "C:", Err: errors.New("Windows could not recycle it (0x80270099)")}}
	_, err := Delete(r)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeRecycleFailed || e.Status() != http.StatusInternalServerError ||
		len(e.Blockers) != 1 || e.Blockers[0].Code != CodeRecycleFailed || !slices.Equal(e.DesktopOff, []string{"a"}) || e.Err == nil {
		t.Fatalf("delete: %v (%+v)", err, e)
	}
	if !exists(a) || len(w.binned()) != 0 {
		t.Fatal("something moved")
	}

	r = w.req(sid, "a")
	r.Bin = &oddBin{Bin: r.Bin, err: &recycle.Error{Path: a, Err: errors.New("not a plain absolute path")}, plan: true}
	p, err := Look(r)
	if err != nil || p.OK || len(p.Blockers) == 0 || p.Blockers[0].Code != CodeRecycleFailed {
		t.Fatalf("look: %+v %v", p, err)
	}
	if strings.Contains(fmt.Sprint(p.Blockers), a) {
		t.Fatalf("the plan names the file: %+v", p.Blockers)
	}
}

// oddBin fails in a way convdel has no words for: at the move, or (plan)
// already when asked whether it takes the items.
type oddBin struct {
	recycle.Bin
	err  error
	plan bool
}

func (b *oddBin) Preflight(paths []string) error {
	if b.plan {
		return b.err
	}
	return b.Bin.Preflight(paths)
}

func (b *oddBin) Recycle(paths []string) (int, error) { return 0, b.err }
