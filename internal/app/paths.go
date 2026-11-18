// Package app defines baton's on-disk layout.
//
//	%LOCALAPPDATA%\baton\            (or $BATON_HOME)
//	  config.json, config.lock       user settings
//	  accounts.json, accounts.lock   account registry (no secrets)
//	  state.json, state.lock         per-account health ledger
//	  desktop.json, desktop.lock     the sessions baton shows in the Claude desktop app
//	  desktop-logins\, desktop-logins.lock
//	                                 the Claude desktop app's sign-ins baton keeps (opt-in)
//	  profiles\<id>\                one CLAUDE_CONFIG_DIR per account, owned by claude.exe
//	  runs\<runId>\                  one supervised session: settings, hook events, journal
package app

import (
	"errors"
	"os"
	"path/filepath"
)

type Paths struct {
	Root string
}

// DefaultPaths honours BATON_HOME (used by tests) and otherwise uses the
// non-roaming %LOCALAPPDATA%\baton.
func DefaultPaths() (Paths, error) {
	if h := os.Getenv("BATON_HOME"); h != "" {
		abs, err := filepath.Abs(h)
		if err != nil {
			return Paths{}, err
		}
		return Paths{Root: abs}, nil
	}
	la := os.Getenv("LOCALAPPDATA")
	if la == "" {
		return Paths{}, errors.New("LOCALAPPDATA is not set")
	}
	return Paths{Root: filepath.Join(la, "baton")}, nil
}

func (p Paths) Config() string           { return filepath.Join(p.Root, "config.json") }
func (p Paths) ConfigLock() string       { return filepath.Join(p.Root, "config.lock") }
func (p Paths) Accounts() string         { return filepath.Join(p.Root, "accounts.json") }
func (p Paths) AccountsLock() string     { return filepath.Join(p.Root, "accounts.lock") }
func (p Paths) State() string            { return filepath.Join(p.Root, "state.json") }
func (p Paths) StateLock() string        { return filepath.Join(p.Root, "state.lock") }
func (p Paths) ProfilesDir() string      { return filepath.Join(p.Root, "profiles") }
func (p Paths) Profile(id string) string { return filepath.Join(p.ProfilesDir(), id) }
func (p Paths) RunsDir() string          { return filepath.Join(p.Root, "runs") }
func (p Paths) Run(id string) string     { return filepath.Join(p.RunsDir(), id) }
func (p Paths) UsageDir() string         { return filepath.Join(p.Root, "usage") }

// Desktop is baton's own list of the sessions it shows in the Claude desktop
// app (the window and every session write it, under DesktopLock).
func (p Paths) Desktop() string     { return filepath.Join(p.Root, "desktop.json") }
func (p Paths) DesktopLock() string { return filepath.Join(p.Root, "desktop.lock") }

// DesktopLogins keeps the Claude desktop app's sign-ins, one folder per
// account, when the user turned switching them on (internal/desklogin).
// Turning it off or uninstalling deletes it.
func (p Paths) DesktopLogins() string     { return filepath.Join(p.Root, "desktop-logins") }
func (p Paths) DesktopLoginsLock() string { return filepath.Join(p.Root, "desktop-logins.lock") }

func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.ProfilesDir(), p.RunsDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
