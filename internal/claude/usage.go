package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"time"
)

// UsageEntrypoint is what a Claude Code started only to ask for its usage
// (ReadUsage) says runs it, in the session registry: ReadSessions leaves such
// an entry out, so a profile never counts as in use for one.
const UsageEntrypoint = "baton-usage"

// usageRequest is the control request claude.exe answers with its /usage
// data (get_usage, part of its SDK mode; Claude Code marks it experimental).
// skip_behaviors leaves out the scan of local transcripts it would add.
const usageRequest = `{"type":"control_request","request_id":"baton-usage","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// ReadUsage asks claude.exe at exe, run in folder dir with env (an account's
// profile), for the plan usage of the account it is signed in to: its answer
// to get_usage, as it came (JSON). claude.exe reads its own sign-in and asks
// the claude.ai usage endpoint, as its /usage command does; baton never
// opens the profile's credentials. No prompt is sent, so it costs none of the
// account's usage. Hooks and MCP servers stay off and nothing is saved as a
// session.
func ReadUsage(exe, dir string, env []string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--strict-mcp-config", "--settings", `{"disableAllHooks":true}`, "--no-session-persistence")
	cmd.Dir = dir
	cmd.Env = append(slices.Clip(ExeEnv(exe, env)), "CLAUDE_CODE_ENTRYPOINT="+UsageEntrypoint)
	cmd.SysProcAttr = hidden()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		// Its input closed, claude.exe ends by itself; one that does not is
		// stopped (baton started it). Waited for only once its output is
		// read: Wait closes the pipe.
		stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	if _, err := io.WriteString(stdin, usageRequest); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		var m struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string          `json:"subtype"`
				RequestID string          `json:"request_id"`
				Error     string          `json:"error"`
				Response  json.RawMessage `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Type != "control_response" || m.Response.RequestID != "baton-usage" {
			continue
		}
		if m.Response.Subtype != "success" {
			return nil, fmt.Errorf("claude get_usage: %s", m.Response.Error)
		}
		return m.Response.Response, nil
	}
	if ctx.Err() != nil {
		return nil, errors.New("claude get_usage: no answer in time")
	}
	return nil, errors.New("claude get_usage: no answer")
}
