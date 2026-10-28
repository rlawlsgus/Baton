package supervisor

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"baton/internal/accounts"
	"baton/internal/config"
	"baton/internal/ledger"
	"baton/internal/run"
)

// setExhausted sets on_exhausted in memory and in config.json, which the
// supervisor reloads while it runs.
func setExhausted(t *testing.T, s *Supervisor, v string) {
	t.Helper()
	s.opt.Config.OnExhausted = v
	if _, err := config.Update(s.opt.Paths.Config(), s.opt.Paths.ConfigLock(), func(c *config.Config) error {
		c.SwitchMode, c.OnExhausted = config.ModeAuto, v
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A run that waits for its first account (every account limited) and is
// then ended by the user exits 0, so its window closes; one that gives up on
// its own does not.
func TestEndingTheFirstWaitIsAUserEnd(t *testing.T) {
	for _, viaCtrlC := range []bool{false, true} {
		s := newTest(t)
		var out bytes.Buffer
		s.opt.Out = &out
		setExhausted(t, s, config.ExhaustedWait)
		s.acct = accounts.Account{}
		s.signals = make(chan os.Signal, 4)
		_, _ = accounts.Update(s.opt.Paths.Accounts(), s.opt.Paths.AccountsLock(), func(r *accounts.Registry) error {
			r.Accounts = append([]accounts.Account{}, s.reg.Accounts...)
			return nil
		})
		_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
			for _, id := range []string{"a", "b"} {
				a := st.Account(id)
				a.Status, a.RetryAt, a.LimitedAt = ledger.StatusExhausted, time.Now().Add(2*time.Hour), time.Now()
			}
			return nil
		})
		type result struct{ code int }
		done := make(chan result, 1)
		go func() { c, _ := s.loop(); done <- result{c} }()
		time.Sleep(700 * time.Millisecond)
		if viaCtrlC {
			s.signals <- os.Interrupt
		} else if err := run.RequestStop(s.runDir); err != nil {
			t.Fatal(err)
		}
		select {
		case r := <-done:
			if r.code != 0 || !s.userEnded || s.abandoned {
				t.Fatalf("ctrl-c=%v: code=%d userEnded=%v abandoned=%v\n%s", viaCtrlC, r.code, s.userEnded, s.abandoned, out.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("ctrl-c=%v: still waiting\n%s", viaCtrlC, out.String())
		}
	}
}

// Every account limited with on_exhausted=stop: baton gives up by itself,
// which must keep the window open for the reason (Options.Attention).
func TestGivingUpNeedsAttention(t *testing.T) {
	s := newTest(t)
	var out bytes.Buffer
	s.opt.Out = &out
	setExhausted(t, s, config.ExhaustedStop)
	_, _ = accounts.Update(s.opt.Paths.Accounts(), s.opt.Paths.AccountsLock(), func(r *accounts.Registry) error {
		r.Accounts = append([]accounts.Account{}, s.reg.Accounts...)
		return nil
	})
	_ = ledger.Update(s.opt.Paths.State(), s.opt.Paths.StateLock(), func(st *ledger.State) error {
		for _, id := range []string{"a", "b"} {
			a := st.Account(id)
			a.Status, a.RetryAt, a.LimitedAt = ledger.StatusExhausted, time.Now().Add(2*time.Hour), time.Now()
		}
		return nil
	})
	if next, ok := s.chooseNext(&pending{reason: reasonLimit}); ok || next != nil {
		t.Fatalf("chooseNext = %v, %v", next, ok)
	}
	if s.userEnded {
		t.Fatal("giving up was taken for a user end")
	}
	if !strings.Contains(out.String(), "every account is limited") {
		t.Fatalf("out: %s", out.String())
	}
}
