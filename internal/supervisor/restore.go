package supervisor

import (
	"strings"

	"baton/internal/run"
)

// owed reports whether the session has work going that a restart would cut
// short: a response, a question in its window, or the continue prompt that
// the current account (or the switch under way) owes.
func (s *Supervisor) owed() bool {
	return s.contOwed || s.owedSwitch || s.watch.status == "busy" || s.watch.status == "waiting"
}

// restoreArgs reopens a session that was cut off: its conversation, in the
// permission mode it was last in, with the options it kept across
// relaunches, and, if it was working, the prompt to go on with the task.
// When another Claude Code has the conversation open by now, it goes on in a
// copy of it; when a baton session does, nothing starts (errInBaton).
func (s *Supervisor) restoreArgs(r *run.Journal) ([]string, error) {
	s.transcr, s.session = r.TranscriptPath, strings.ToLower(r.SessionID)
	copyIt, err := s.startHeld(r.TranscriptPath, s.session)
	if err != nil {
		return nil, err
	}
	// The mode the run saved as its own: the transcript's last mode row can
	// be another Claude Code's by now (a terminal's claude writes one even
	// when it only opens the conversation and quits). A run from before baton
	// kept it has only the transcript.
	mode := r.PermissionMode
	if mode == "" {
		mode = s.permissionMode(r.TranscriptPath)
	}
	args := append([]string{}, s.opt.ClaudeArgs.Persistent...)
	args = s.withMode(args, mode)
	args = append(args, "--resume", r.TranscriptPath)
	if copyIt {
		args = append(args, "--fork-session")
		s.forkSource = r.TranscriptPath
		s.transcr, s.session = "", ""
	}
	if r.Owed {
		args = append(args, restartPrompt)
	}
	// Reopened: the old run is no longer offered.
	dir := s.opt.Paths.Run(r.RunID)
	if j, err := run.LoadJournal(dir); err == nil {
		j.RestoredBy = s.runID
		if err := run.SaveJournal(dir, j); err != nil {
			s.logf("restore: %v", err)
		}
	}
	s.logf("restoring run %s (owed %v, copy %v)", r.RunID, r.Owed, copyIt)
	return args, nil
}
