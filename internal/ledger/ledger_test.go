package ledger

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUpdateIsSerialized(t *testing.T) {
	dir := t.TempDir()
	path, lock := filepath.Join(dir, "state.json"), filepath.Join(dir, "state.lock")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Update(path, lock, func(s *State) error {
				s.LogSwitch(Switch{At: time.Now(), From: "a", To: "b", Reason: "test"})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s, err := Load(path)
	if err != nil || len(s.Switches) != 20 {
		t.Fatalf("lost updates: %d switches, err=%v", len(s.Switches), err)
	}
}

func TestAccountDefaultsAndLogCap(t *testing.T) {
	var s State
	if s.StatusOf("x").Status != StatusOK {
		t.Fatal("unknown account must default to ok")
	}
	s.Account("x").Status = StatusExhausted
	if s.StatusOf("x").Status != StatusExhausted {
		t.Fatal("status not stored")
	}
	for i := 0; i < maxSwitchLog+50; i++ {
		s.LogSwitch(Switch{})
	}
	if len(s.Switches) != maxSwitchLog {
		t.Fatalf("log not capped: %d", len(s.Switches))
	}
}
