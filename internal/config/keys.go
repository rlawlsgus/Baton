package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Keys are the settings `baton config` and the GUI read and change, in
// display order.
var Keys = []string{"switch_mode", "on_exhausted", "cooldown_seconds", "max_switches_per_hour",
	"exhausted_retry_minutes", "continue_prompt", "share_user_config", "claude_path", "allow_desktop_bundle", "remote_control", "notifications",
	"keep_awake", "check_updates", "gui_language"}

// ErrNeedsAcknowledgement: turning switching on needs the risk notice accepted
// first (see DisclosureVersion).
var ErrNeedsAcknowledgement = errors.New("the risk notice has to be accepted before baton switches accounts")

// Get returns one setting as text.
func (c Config) Get(key string) (string, bool) {
	switch key {
	case "switch_mode":
		return c.SwitchMode, true
	case "on_exhausted":
		return c.OnExhausted, true
	case "cooldown_seconds":
		return strconv.Itoa(c.CooldownSeconds), true
	case "max_switches_per_hour":
		return strconv.Itoa(c.MaxSwitchesPerHour), true
	case "exhausted_retry_minutes":
		return strconv.Itoa(c.ExhaustedRetryMinutes), true
	case "continue_prompt":
		return c.ContinuePrompt, true
	case "share_user_config":
		return strconv.FormatBool(c.ShareEnabled()), true
	case "remote_control":
		return strconv.FormatBool(c.RemoteControl), true
	case "notifications":
		return c.NotifyLevel(), true
	case "keep_awake":
		return strconv.FormatBool(c.KeepAwakeOn()), true
	case "claude_path":
		return c.ClaudePath, true
	case "allow_desktop_bundle":
		return strconv.FormatBool(c.DesktopBundleAllowed()), true
	case "check_updates":
		return strconv.FormatBool(c.UpdateChecks()), true
	case "gui_language":
		if c.GUILanguage == "" {
			return "auto", true
		}
		return c.GUILanguage, true
	}
	return "", false
}

// Setter checks value for key (as current is now) and returns the change to
// apply to a freshly loaded config under the lock, so a slow question in
// between cannot undo another command's change. acknowledged says the user
// has just accepted the risk notice.
func Setter(current Config, key, value string, acknowledged bool) (func(*Config), error) {
	positive := func() (int, error) {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("%s must be a positive number", key)
		}
		return n, nil
	}
	boolean := func() (bool, error) {
		b, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, fmt.Errorf("%s must be true or false", key)
		}
		return b, nil
	}
	switch key {
	case "switch_mode":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != ModeAuto && v != ModeConfirm && v != ModeOff {
			return nil, errors.New("switch_mode must be auto, confirm or off")
		}
		if v != ModeOff && current.RiskAcknowledged < DisclosureVersion && !acknowledged {
			return nil, ErrNeedsAcknowledgement
		}
		return func(c *Config) {
			if acknowledged {
				c.RiskAcknowledged = DisclosureVersion
			}
			c.SwitchMode = v
		}, nil
	case "on_exhausted":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != ExhaustedWait && v != ExhaustedStop {
			return nil, errors.New("on_exhausted must be wait or stop")
		}
		return func(c *Config) { c.OnExhausted = v }, nil
	case "cooldown_seconds", "max_switches_per_hour", "exhausted_retry_minutes":
		n, err := positive()
		if err != nil {
			return nil, err
		}
		return func(c *Config) {
			switch key {
			case "cooldown_seconds":
				c.CooldownSeconds = n
			case "max_switches_per_hour":
				c.MaxSwitchesPerHour = n
			default:
				c.ExhaustedRetryMinutes = n
			}
		}, nil
	case "continue_prompt":
		return func(c *Config) { c.ContinuePrompt = value }, nil
	case "remote_control":
		b, err := boolean()
		if err != nil {
			return nil, err
		}
		return func(c *Config) { c.RemoteControl = b }, nil
	case "notifications":
		v := strings.ToLower(strings.TrimSpace(value))
		if v != NotifyAll && v != NotifyImportant && v != NotifyOff {
			return nil, errors.New("notifications must be all, important or off")
		}
		return func(c *Config) { c.Notifications = v }, nil
	case "keep_awake":
		b, err := boolean()
		if err != nil {
			return nil, err
		}
		return func(c *Config) { c.KeepAwake = &b }, nil
	case "share_user_config":
		b, err := boolean()
		if err != nil {
			return nil, err
		}
		return func(c *Config) { c.ShareUserConfig = &b }, nil
	case "allow_desktop_bundle":
		b, err := boolean()
		if err != nil {
			return nil, err
		}
		return func(c *Config) { c.AllowDesktopBundle = &b }, nil
	case "check_updates":
		b, err := boolean()
		if err != nil {
			return nil, err
		}
		return func(c *Config) { c.CheckUpdates = &b }, nil
	case "claude_path":
		v := strings.Trim(strings.TrimSpace(value), `"`)
		if v == "" || strings.EqualFold(v, "auto") || strings.EqualFold(v, "none") {
			// Back to auto-detect ("auto" because PowerShell 5.1 cannot pass "").
			return func(c *Config) { c.ClaudePath = "" }, nil
		}
		if strings.EqualFold(v, "desktop") {
			// Always the Claude desktop app's own Claude Code.
			return func(c *Config) { c.ClaudePath = "desktop" }, nil
		}
		// Stored absolute: a relative path would mean a different file in every
		// folder baton runs in.
		abs, err := filepath.Abs(v)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
			return nil, fmt.Errorf("%s is not a file", abs)
		}
		return func(c *Config) { c.ClaudePath = abs }, nil
	case "gui_language":
		v := strings.ToLower(strings.TrimSpace(value))
		switch v {
		case "auto", "":
			v = ""
		case "ko", "en":
		default:
			return nil, errors.New("gui_language must be auto, ko or en")
		}
		return func(c *Config) { c.GUILanguage = v }, nil
	}
	return nil, fmt.Errorf("unknown key %q (keys: %s)", key, strings.Join(Keys, ", "))
}
