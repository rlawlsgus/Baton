package envx

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildScrubsAuthAndHostVars(t *testing.T) {
	base := []string{
		"PATH=C:\\bin", "HTTPS_PROXY=http://proxy:8080", "=C:=C:\\x",
		"ANTHROPIC_API_KEY=sk-x", "anthropic_base_url=http://gw", "CLAUDE_CODE_OAUTH_TOKEN=t",
		"CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR=3", "CLAUDE_CODE_HOST_SESSION_ID=h", "CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=s", "CLAUDE_CONFIG_DIR=C:\\old", "CLAUDE_CODE_MAX_OUTPUT_TOKENS=9000",
		"CLAUDE_CODE_GIT_BASH_PATH=C:\\git\\bash.exe",
	}
	env, removed := Build(base, map[string]string{"CLAUDE_CONFIG_DIR": `C:\new`})
	got := strings.Join(env, "\n")
	for _, keep := range []string{"PATH=C:\\bin", "HTTPS_PROXY=http://proxy:8080", "=C:=C:\\x",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=9000", "CLAUDE_CODE_GIT_BASH_PATH=C:\\git\\bash.exe", `CLAUDE_CONFIG_DIR=C:\new`} {
		if !strings.Contains(got, keep) {
			t.Errorf("missing %q", keep)
		}
	}
	for _, gone := range []string{"sk-x", "http://gw", "OAUTH_TOKEN=t", "HOST_SESSION", "CLAUDECODE=1", "SESSION_ID=s", `C:\old`} {
		if strings.Contains(got, gone) {
			t.Errorf("%q should be scrubbed", gone)
		}
	}
	if !reflect.DeepEqual(Notable(removed), []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR", "anthropic_base_url"}) {
		t.Errorf("Notable = %q", Notable(removed))
	}
}

// Claude Code's opt-in transcript trimming replaces the file by a rename:
// a conversation baton shows in the Claude desktop app has two names, and a
// rename would split them, so it never reaches baton's claude.
func TestTranscriptTrimmingIsKeptOut(t *testing.T) {
	env, removed := Build([]string{"PATH=x", "claude_code_transcript_local_gc=1"}, nil)
	if strings.Contains(strings.Join(env, "\n"), "LOCAL_GC") || len(removed) != 1 || len(Notable(removed)) != 0 {
		t.Fatalf("env %q removed %q", env, removed)
	}
}
