package run

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

func TestTailReadsOnlyNewCompleteLines(t *testing.T) {
	dir := t.TempDir()
	tail := NewTail(dir)
	if evs := tail.Next(); len(evs) != 0 {
		t.Fatal("no file yet")
	}
	payload, _ := json.Marshal(map[string]string{"session_id": "s1", "error": "rate_limit"})
	_ = AppendEvent(dir, Event{TS: time.Now(), Event: "SessionStart", ClaudePID: 7, Payload: payload})
	_ = AppendEvent(dir, Event{TS: time.Now(), Event: "StopFailure", ClaudePID: 7, Payload: payload})
	evs := tail.Next()
	if len(evs) != 2 || evs[1].Event != "StopFailure" || evs[1].Decode().Error != "rate_limit" || evs[0].ClaudePID != 7 {
		t.Fatalf("got %+v", evs)
	}
	if evs := tail.Next(); len(evs) != 0 {
		t.Fatalf("re-read old events: %+v", evs)
	}
}

func TestSwitchRequestIsConsumedOnce(t *testing.T) {
	dir := t.TempDir()
	if req, err := TakeSwitch(dir); req != nil || err != nil {
		t.Fatal("no request expected")
	}
	_ = RequestSwitch(dir, SwitchRequest{Target: "work", At: time.Now()})
	req, err := TakeSwitch(dir)
	if err != nil || req == nil || req.Target != "work" {
		t.Fatalf("req=%+v err=%v", req, err)
	}
	if req, _ := TakeSwitch(dir); req != nil {
		t.Fatal("request delivered twice")
	}
}

func TestIDs(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(NewSessionID()) {
		t.Fatal("bad uuid")
	}
	if a, b := NewID(time.Now()), NewID(time.Now()); a == b {
		t.Fatal("run ids collide")
	}
}

func TestActiveFiltersDeadAndDone(t *testing.T) {
	runs := t.TempDir()
	live := Journal{RunID: "r1", SupervisorPID: 1, State: StateRunning, Started: time.Now()}
	done := Journal{RunID: "r2", SupervisorPID: 2, State: StateDone}
	dead := Journal{RunID: "r3", SupervisorPID: 3, State: StateRunning}
	for _, j := range []Journal{live, done, dead} {
		if err := SaveJournal(runs+"/"+j.RunID, j); err != nil {
			t.Fatal(err)
		}
	}
	got := Active(runs, func(pid int, _ time.Time) bool { return pid != 3 })
	if len(got) != 1 || got[0].RunID != "r1" {
		t.Fatalf("got %+v", got)
	}
}
