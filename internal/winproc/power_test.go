package winproc

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAwake(t *testing.T) {
	a := &Awake{Reason: "baton test: keeping the PC awake"}
	if err := a.Set(false); err != nil || a.On() {
		t.Fatalf("off before on: %v", err)
	}
	if err := a.Set(true); err != nil || !a.On() {
		t.Fatalf("on: %v", err)
	}
	// Windows lists it (powercfg needs an administrator; skip the check otherwise).
	if out, err := exec.Command("powercfg", "/requests").CombinedOutput(); err == nil {
		if !strings.Contains(string(out), "baton test") {
			t.Errorf("powercfg /requests does not list it:\n%s", out)
		}
	} else {
		t.Logf("powercfg /requests: %v %s", err, strings.TrimSpace(string(out)))
	}
	if err := a.Set(false); err != nil || a.On() {
		t.Fatalf("off: %v", err)
	}
	a.Close()
	if err := a.Set(false); err != nil {
		t.Fatal(err)
	}
}
