package supervisor

import (
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
)

// A folder rule keeps the session on its accounts: no switch to another, by
// baton or by request, and no start on one either.
func TestFolderRuleKeepsTheSessionOnItsAccounts(t *testing.T) {
	s := newTest(t)
	s.opt.Out = io.Discard
	s.reg.Accounts = append(s.reg.Accounts, accounts.Account{ID: "c", ProfileDir: s.reg.Accounts[0].ProfileDir + "-c", Enabled: true, LoggedIn: true, Priority: 30})
	if err := accounts.Save(s.opt.Paths.Accounts(), s.reg); err != nil {
		t.Fatal(err)
	}
	s.rule = &config.FolderRule{Folder: s.opt.Cwd, Accounts: []string{"a", "b"}}
	if s.manualTargetUsable("c") || !s.manualTargetUsable("b") {
		t.Fatal("switch requests ignore the rule")
	}
	// c has the highest priority, but only a and b may be used.
	if got := s.choosable(); len(got.Accounts) != 2 || got.Get("c") != nil {
		t.Fatalf("choosable %+v", got.Accounts)
	}
	next, _, ok := s.pickWithWait("a", "")
	if !ok || next.ID != "b" {
		t.Fatalf("picked %+v", next)
	}
	// With b limited too, the session stays put instead of going to c.
	_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
		*st.Account("a") = ledger.AccountState{Status: ledger.StatusExhausted, RetryAt: time.Now().Add(time.Hour)}
		*st.Account("b") = ledger.AccountState{Status: ledger.StatusExhausted, RetryAt: time.Now().Add(2 * time.Hour)}
		return nil
	})
	s.opt.Config.OnExhausted = config.ExhaustedStop
	if next, _, ok := s.pickWithWait("a", ""); ok {
		t.Fatalf("went on on %s although every allowed account is limited", next.ID)
	}
	s.opt.Account = "c"
	if _, ok := s.initialAccount(); ok {
		t.Fatal("started on an account the rule does not allow")
	}
}

// A running session takes on a stricter rule set meanwhile, never a looser one.
func TestFolderRuleOnlyTightensMidRun(t *testing.T) {
	s := newTest(t)
	set := func(accts ...string) {
		_, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
			c.SwitchMode = config.ModeAuto
			c.FolderRules = nil
			if len(accts) > 0 {
				return c.SetRule(s.opt.Cwd, accts)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		s.reloadConfig()
	}
	set("a", "b")
	if s.rule == nil || !slices.Equal(s.rule.Accounts, []string{"a", "b"}) {
		t.Fatalf("a new rule: %+v", s.rule)
	}
	set("a")
	set("a", "b") // looser again
	set()         // and removed
	if s.rule == nil || !slices.Equal(s.rule.Accounts, []string{"a"}) {
		t.Fatalf("rule loosened mid-run: %+v", s.rule)
	}
}

// A cut-off session reopens in its conversation and permission mode, with
// its options, goes on with the task it was doing, and its old run is no
// longer offered. It starts on the account it was on when that one is free.
func TestRestoreArgs(t *testing.T) {
	s := newTest(t, "--model", "opus")
	s.runID = "20261029-120000-bbbbbb"
	tr := writeTranscript(t, s.reg.Accounts[1].ProfileDir, "11111111-2222-4333-8444-555555555555")
	appendRow(t, tr, "acceptEdits")
	old := run.Journal{RunID: "20261029-110000-aaaaaa", SessionID: "11111111-2222-4333-8444-555555555555", TranscriptPath: tr,
		Account: "b", Cwd: s.opt.Cwd, State: run.StateRunning, Owed: true, Args: []string{"--model", "opus"}}
	_ = os.MkdirAll(s.opt.Paths.Run(old.RunID), 0o700)
	_ = run.SaveJournal(s.opt.Paths.Run(old.RunID), old)
	s.opt.Restore = &old
	args, err := s.firstArgs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--model", "opus", "--permission-mode", "acceptEdits", "--resume", tr, restartPrompt}
	if !slices.Equal(args, want) {
		t.Fatalf("args %q\nwant %q", args, want)
	}
	if j, _ := run.LoadJournal(s.opt.Paths.Run(old.RunID)); j.RestoredBy != s.runID {
		t.Fatalf("old run not marked: %+v", j)
	}
	s.opt.Prefer = "b"
	if a, ok := s.initialAccount(); !ok || a.ID != "b" {
		t.Fatalf("started on %+v, want the account it was on", a)
	}
	// Idle when it was cut off: no prompt.
	old.Owed = false
	if args, _ := s.firstArgs(); slices.Contains(args, restartPrompt) {
		t.Fatalf("an idle session got the prompt: %q", args)
	}
}
