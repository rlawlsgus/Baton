package supervisor

import (
	"fmt"

	"baton/internal/claude"
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
func (s *Supervisor) restoreArgs(r *run.Journal) ([]string, error) {
	s.transcr, s.session = r.TranscriptPath, r.SessionID
	if h := claude.Holders(s.holderDirs(), s.session); len(h) > 0 {
		return nil, fmt.Errorf("this session is already open in another claude window (pid %d); close it first", h[0].PID)
	}
	args := append([]string{}, s.opt.ClaudeArgs.Persistent...)
	args = s.withMode(args, s.permissionMode(r.TranscriptPath))
	args = append(args, "--resume", r.TranscriptPath)
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
	s.logf("restoring run %s (owed %v)", r.RunID, r.Owed)
	return args, nil
}
