package config

import (
	"errors"
	"slices"
	"testing"
)

// Switching the desktop app's sign-in is off until turned on with its own
// notice accepted. The plain setter never turns it on or off: on goes
// through desklogin.TurnOn (DesktopLoginsOnSetter, after its checks), off
// through desklogin.TurnOff.
func TestDesktopLoginsSetting(t *testing.T) {
	c := Defaults()
	if v, ok := c.Get("desktop_logins"); !ok || v != "false" || c.DesktopLoginsOn() {
		t.Fatalf("default: %q %v", v, c.DesktopLoginsOn())
	}
	if !slices.Contains(Keys, "desktop_logins") {
		t.Fatal("desktop_logins is not in Keys")
	}
	for _, ack := range []bool{false, true} {
		for _, v := range []string{"true", " True ", "1"} {
			if _, err := Setter(c, "desktop_logins", v, ack); !errors.Is(err, ErrDesktopLoginsTurnOn) {
				t.Fatalf("%q (acknowledged %v): %v", v, ack, err)
			}
		}
		for _, v := range []string{"false", "0", " FALSE "} {
			if _, err := Setter(c, "desktop_logins", v, ack); !errors.Is(err, ErrDesktopLoginsTurnOff) {
				t.Fatalf("%q (acknowledged %v): %v", v, ack, err)
			}
		}
	}
	if _, err := Setter(c, "desktop_logins", "maybe", true); err == nil || errors.Is(err, ErrDesktopLoginsTurnOff) || errors.Is(err, ErrDesktopLoginsTurnOn) {
		t.Fatalf("maybe: %v", err)
	}

	if _, err := DesktopLoginsOnSetter(c, false); !errors.Is(err, ErrNeedsDesktopAcknowledgement) {
		t.Fatalf("on without the notice: %v", err)
	}
	// The account-switching notice is another notice.
	c.RiskAcknowledged = DisclosureVersion
	if _, err := DesktopLoginsOnSetter(c, false); !errors.Is(err, ErrNeedsDesktopAcknowledgement) {
		t.Fatalf("on with only the risk notice: %v", err)
	}
	set, err := DesktopLoginsOnSetter(c, true)
	if err != nil {
		t.Fatal(err)
	}
	set(&c)
	if !c.DesktopLogins || c.DesktopLoginsAcknowledged != DesktopLoginsDisclosure || !c.DesktopLoginsOn() || c.Validate() != nil {
		t.Fatalf("on: %+v", c)
	}
	if v, _ := c.Get("desktop_logins"); v != "true" {
		t.Fatalf("get: %s", v)
	}
	// Accepted once: on again needs no question.
	c.DesktopLogins = false
	if set, err = DesktopLoginsOnSetter(c, false); err != nil {
		t.Fatal(err)
	}
	set(&c)
	if !c.DesktopLoginsOn() {
		t.Fatalf("on again: %+v", c)
	}
	// An older notice than the one now is not enough, and `baton config`
	// shows it as it works: off.
	c.DesktopLoginsAcknowledged = DesktopLoginsDisclosure - 1
	if c.DesktopLoginsOn() {
		t.Fatal("on with an older notice")
	}
	if v, _ := c.Get("desktop_logins"); v != "false" {
		t.Fatalf("get with an older notice: %s", v)
	}
	if _, err := DesktopLoginsOnSetter(c, false); !errors.Is(err, ErrNeedsDesktopAcknowledgement) {
		t.Fatalf("older notice: %v", err)
	}
}
