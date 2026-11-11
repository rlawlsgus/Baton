package main

import (
	"errors"
	"fmt"
	"strings"

	"baton/internal/app"
	"baton/internal/notify"
	"baton/internal/run"
	"baton/internal/winui"
)

// cmdFocus brings a session's window to the front. A click on one of baton's
// notifications runs `baton-app.exe focus baton-session:<run id>`; when that
// session's window is gone, and for baton-session:open, baton's own window
// opens instead.
func cmdFocus(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: baton focus <run id>")
	}
	id := strings.Trim(strings.TrimPrefix(args[0], notify.Scheme+":"), "/")
	if id != notify.OpenWindow {
		if !run.ValidID(id) {
			return fmt.Errorf("not a baton session: %q", args[0])
		}
		p, err := app.DefaultPaths()
		if err != nil {
			return err
		}
		if j, err := run.LoadJournal(p.Run(id)); err == nil && j.ConsoleWindow != 0 && supervisorAlive(j.SupervisorPID, j.SupervisorCreated) {
			how, err := winui.FocusWindowHow(uintptr(j.ConsoleWindow))
			if err == nil {
				guiLog("focus %s: %s", id, how)
				return nil
			}
			guiLog("focus %s: %v", id, err)
			if errors.Is(err, winui.ErrFocusRefused) {
				// The session's window flashes on the taskbar; baton's own
				// window is not the one the user asked for.
				return nil
			}
		}
	}
	// This answers the user's click: let baton's window come to the front.
	winui.Nudge()
	if guiBuild == "1" {
		return cmdGUI(nil)
	}
	if startApp() {
		return nil
	}
	return winui.ErrNoWindow
}
