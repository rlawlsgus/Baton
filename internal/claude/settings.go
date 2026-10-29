package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"baton/internal/fsx"
)

// HookEvents baton listens to. StopFailure has no matcher so baton sees every
// API error and classifies it itself (rate_limit vs server_error vs auth);
// Stop tells baton a turn (for example the auto-sent continue prompt) finished.
var HookEvents = []string{"SessionStart", "Stop", "StopFailure", "SessionEnd"}

// RunSettings is the launcher-owned file passed with --settings. Hooks given
// this way merge with the user's own hooks. A statusLine is a single value
// and would replace the user's, so it comes only through extra, when the
// user has none (see UserStatusLine). extra also adds settings baton carries
// over between accounts (see BypassAccepted).
func RunSettings(batonExe, runDir string, extra map[string]any) map[string]any {
	hooks := map[string]any{}
	for _, ev := range HookEvents {
		hooks[ev] = []any{map[string]any{
			"hooks": []any{map[string]any{
				// Exec form: no shell, argv passed verbatim (spaces/Hangul safe).
				// Never put "${...}" in args; claude substitutes it.
				"type":    "command",
				"command": batonExe,
				"args":    []string{"hook", ev, "--run-dir", runDir},
			}},
		}}
	}
	s := map[string]any{
		"hooks": hooks,
		// claude's own "wait for the reset" would keep the session asleep
		// instead of ending the turn, hiding the limit from baton.
		"autoContinueAtUsageLimit": false,
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func WriteRunSettings(path, batonExe, runDir string, extra map[string]any) error {
	return fsx.WriteJSONAtomic(path, RunSettings(batonExe, runDir, extra))
}

// BypassAccepted reports whether the user has accepted claude's "Bypass
// Permissions mode" warning in any of these config folders. A session started
// with --dangerously-skip-permissions would otherwise stop on that warning
// (default answer: exit) on every account that has not seen it yet.
func BypassAccepted(dirs ...string) bool {
	for _, d := range dirs {
		data, err := os.ReadFile(filepath.Join(d, "settings.json"))
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(fsx.StripBOM(data), &m) == nil {
			if v, _ := m["skipDangerousModePermissionPrompt"].(bool); v {
				return true
			}
		}
	}
	return false
}

// LastPermissionMode is the permission mode the session last ran in, from
// the transcript's "permission-mode" rows. claude does not restore it on a
// resume, so a relaunch passes it explicitly.
func LastPermissionMode(transcript string) string {
	return LastPermissionModeFrom(transcript, 0)
}

// LastPermissionModeFrom is LastPermissionMode over the rows written after
// byte offset from (a row cut by the offset does not parse and is skipped).
func LastPermissionModeFrom(transcript string, from int64) string {
	f, err := os.Open(transcript)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return ""
	}
	mode := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	marker := []byte(`"permission-mode"`)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, marker) {
			continue
		}
		var row struct {
			Type string `json:"type"`
			Mode string `json:"permissionMode"`
		}
		if json.Unmarshal(line, &row) == nil && row.Type == "permission-mode" && row.Mode != "" {
			mode = row.Mode
		}
	}
	return mode
}
