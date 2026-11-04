package config

import (
	"path/filepath"
	"sync"
	"testing"
)

// Two commands changing different settings at once must both win.
func TestUpdateKeepsConcurrentChanges(t *testing.T) {
	dir := t.TempDir()
	path, lock := filepath.Join(dir, "config.json"), filepath.Join(dir, "config.lock")
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := Update(path, lock, func(c *Config) error {
				switch i % 4 {
				case 0:
					c.CooldownSeconds = 99
				case 1:
					c.MaxSwitchesPerHour = 3
				case 2:
					c.OnExhausted = ExhaustedStop
				default:
					c.ContinuePrompt = "go on"
				}
				return nil
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.CooldownSeconds != 99 || c.MaxSwitchesPerHour != 3 || c.OnExhausted != ExhaustedStop || c.ContinuePrompt != "go on" {
		t.Fatalf("lost an update: %+v", c)
	}
}

func TestSetterAndGet(t *testing.T) {
	c := Defaults()
	if _, err := Setter(c, "switch_mode", "auto", false); err != ErrNeedsAcknowledgement {
		t.Fatalf("auto without ack: %v", err)
	}
	set, err := Setter(c, "switch_mode", " Confirm ", true)
	if err != nil {
		t.Fatal(err)
	}
	set(&c)
	if c.SwitchMode != ModeConfirm || c.RiskAcknowledged != DisclosureVersion {
		t.Fatalf("%+v", c)
	}
	for key, bad := range map[string]string{"on_exhausted": "later", "cooldown_seconds": "0", "share_user_config": "maybe",
		"gui_language": "fr", "gui_theme": "pink", "claude_path": `C:\nope\claude.exe`, "notifications": "some", "keep_awake": "yes please",
		"office": "fun", "nope": "x"} {
		if _, err := Setter(c, key, bad, false); err == nil {
			t.Errorf("%s=%q accepted", key, bad)
		}
	}
	// The window's colours: as Windows has them until chosen, and back.
	if v, _ := Defaults().Get("gui_theme"); v != "auto" {
		t.Fatalf("theme default: %s", v)
	}
	for _, v := range []string{" Dark ", "auto"} {
		set, err = Setter(c, "gui_theme", v, false)
		if err != nil {
			t.Fatal(err)
		}
		set(&c)
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if c.GUITheme != "" {
		t.Fatalf("theme after auto: %q", c.GUITheme)
	}
	set, _ = Setter(c, "allow_desktop_bundle", "false", false)
	set(&c)
	if v, _ := c.Get("allow_desktop_bundle"); v != "false" || c.DesktopBundleAllowed() {
		t.Fatalf("bundle: %s", v)
	}
	if v, _ := Defaults().Get("allow_desktop_bundle"); v != "true" {
		t.Fatalf("bundle default: %s", v)
	}
	// Notifications and staying awake are on until turned off.
	if v, _ := Defaults().Get("notifications"); v != NotifyAll || !Defaults().KeepAwakeOn() {
		t.Fatalf("defaults: notifications %s, keep awake %v", v, Defaults().KeepAwakeOn())
	}
	set, _ = Setter(c, "notifications", " Important ", false)
	set(&c)
	set, _ = Setter(c, "keep_awake", "false", false)
	set(&c)
	if c.NotifyLevel() != NotifyImportant || c.KeepAwakeOn() || c.Validate() != nil {
		t.Fatalf("notifications %q, keep awake %v", c.Notifications, c.KeepAwakeOn())
	}
	for _, k := range Keys {
		if _, ok := c.Get(k); !ok {
			t.Errorf("Get(%q) missing", k)
		}
	}
}

// The office is a toy: off until turned on, and saved like any setting.
func TestOfficeSetting(t *testing.T) {
	c := Defaults()
	if v, ok := c.Get("office"); !ok || v != "false" || c.Office {
		t.Fatalf("default: %q %v", v, c.Office)
	}
	set, err := Setter(c, "office", "true", false)
	if err != nil {
		t.Fatal(err)
	}
	set(&c)
	if v, _ := c.Get("office"); v != "true" || !c.Office || c.Validate() != nil {
		t.Fatalf("on: %q", v)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if _, err := Update(path, path+".lock", func(cfg *Config) error { cfg.Office = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || !got.Office {
		t.Fatalf("saved: %+v %v", got, err)
	}
}
