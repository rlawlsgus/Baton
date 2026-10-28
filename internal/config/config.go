// Package config holds the user's baton settings (config.json).
package config

import (
	"errors"
	"fmt"
	"os"

	"baton/internal/fsx"
)

const (
	ModeAuto    = "auto"    // switch without asking
	ModeConfirm = "confirm" // ask in the console before each switch
	ModeOff     = "off"     // never switch automatically; `baton switch` still works

	ExhaustedWait = "wait" // wait for the soonest reset, then continue
	ExhaustedStop = "stop" // stop and print how to resume
)

const DefaultContinuePrompt = "The previous session stopped at a usage limit and is now continuing on another account. " +
	"Continue the task you were working on; do not repeat work that is already complete."

type Config struct {
	// ClaudePath pins claude.exe; empty means auto-detect.
	ClaudePath string `json:"claude_path,omitempty"`
	// AllowDesktopBundle lets auto-detect fall back to the Claude Code bundled
	// with the Claude desktop app (its path changes with every app update, so
	// baton looks it up again each time). Default on: most users have the app,
	// not the CLI. nil means default.
	AllowDesktopBundle *bool `json:"allow_desktop_bundle,omitempty"`

	// SwitchMode is empty until the user has seen the disclosure and chosen.
	SwitchMode string `json:"switch_mode,omitempty"`
	// OnExhausted decides what happens when every account is limited.
	OnExhausted string `json:"on_exhausted"`
	// CooldownSeconds is the minimum gap between two switches in one run.
	CooldownSeconds int `json:"cooldown_seconds"`
	// MaxSwitchesPerHour caps switches in one run.
	MaxSwitchesPerHour int `json:"max_switches_per_hour"`
	// ExhaustedRetryMinutes is used when the limit message has no reset time.
	ExhaustedRetryMinutes int `json:"exhausted_retry_minutes"`
	// ContinuePrompt is submitted on the new account after a limit switch.
	ContinuePrompt string `json:"continue_prompt"`
	// ShareUserConfig links/copies the user's normal ~/.claude setup (skills,
	// agents, commands, settings, CLAUDE.md) into every profile. Default on.
	ShareUserConfig *bool `json:"share_user_config,omitempty"`
	// RiskAcknowledged is the DisclosureVersion the user last accepted.
	RiskAcknowledged int `json:"risk_acknowledged,omitempty"`
	// GUILanguage is the GUI's language: "" (follow Windows), "ko" or "en".
	GUILanguage string `json:"gui_language,omitempty"`
}

// DisclosureVersion changes whenever the risk notice changes materially, so
// the user sees the new wording before switching continues.
const DisclosureVersion = 2

func (c Config) ShareEnabled() bool { return c.ShareUserConfig == nil || *c.ShareUserConfig }

func (c Config) DesktopBundleAllowed() bool {
	return c.AllowDesktopBundle == nil || *c.AllowDesktopBundle
}

func Defaults() Config {
	return Config{
		OnExhausted:           ExhaustedWait,
		CooldownSeconds:       60,
		MaxSwitchesPerHour:    6,
		ExhaustedRetryMinutes: 60,
		ContinuePrompt:        DefaultContinuePrompt,
	}
}

func Load(path string) (Config, error) {
	c := Defaults()
	err := fsx.ReadJSON(path, &c)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	d := Defaults()
	if c.OnExhausted == "" {
		c.OnExhausted = d.OnExhausted
	}
	if c.CooldownSeconds <= 0 {
		c.CooldownSeconds = d.CooldownSeconds
	}
	if c.MaxSwitchesPerHour <= 0 {
		c.MaxSwitchesPerHour = d.MaxSwitchesPerHour
	}
	if c.ExhaustedRetryMinutes <= 0 {
		c.ExhaustedRetryMinutes = d.ExhaustedRetryMinutes
	}
	if c.ContinuePrompt == "" {
		c.ContinuePrompt = d.ContinuePrompt
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	switch c.SwitchMode {
	case "", ModeAuto, ModeConfirm, ModeOff:
	default:
		return fmt.Errorf("config: switch_mode must be auto, confirm or off (got %q)", c.SwitchMode)
	}
	switch c.OnExhausted {
	case ExhaustedWait, ExhaustedStop:
	default:
		return fmt.Errorf("config: on_exhausted must be wait or stop (got %q)", c.OnExhausted)
	}
	switch c.GUILanguage {
	case "", "ko", "en":
	default:
		return fmt.Errorf("config: gui_language must be ko or en (got %q)", c.GUILanguage)
	}
	return nil
}

func Save(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return fsx.WriteJSONAtomic(path, c)
}

// Update applies fn to config.json under a lock, so two baton commands that
// change different settings at the same time do not undo each other.
func Update(path, lockPath string, fn func(*Config) error) (Config, error) {
	unlock, err := fsx.Lock(lockPath)
	if err != nil {
		return Config{}, err
	}
	defer unlock()
	c, err := Load(path)
	if err != nil {
		return c, err
	}
	if err := fn(&c); err != nil {
		return c, err
	}
	return c, Save(path, c)
}
