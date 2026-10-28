package claude

import (
	"fmt"
	"strings"
)

// Args is the user's claude command line split by how a relaunch treats it.
type Args struct {
	Original   []string // the user's tokens in their own order, for the first launch
	Persistent []string // options kept on every relaunch (--model, --add-dir, ...)
	FirstOnly  []string // session selection, start-only options and the prompt
	Resumes    bool     // the first launch resumes an existing session (-r / -c / --from-pr)
	ResumeArg  string   // value given to -r/--resume, if any
	Continue   bool     // -c / --continue was given
}

// Option tables follow claude 2.1.308 (`claude --help` plus hidden options).
// claude uses commander: a required option-argument takes the next token even
// if it starts with '-', and a variadic one takes every following non-option
// token, so baton must group them exactly the same way.
var (
	valueFlags = set("--agent", "--agents", "--append-system-prompt", "--append-system-prompt-file",
		"--autocompact", "--debug-file", "--effort", "--fallback-model", "--input-format", "--json-schema",
		"--max-budget-usd", "--model", "-n", "--name", "--output-format", "--permission-mode",
		"--permission-prompts", "--plugin-url", "--remote-control-session-name-prefix", "--session-id",
		"--setting-sources", "--system-prompt", "--system-prompt-file", "--system-prompt-snapshot",
		"--plugin-dir",
		// hidden
		"--thinking", "--thinking-display", "--fast-mode", "--max-thinking-tokens", "--permission-prompt-tool",
		"--advisor", "--proactivity", "--plan-mode-instructions", "--resume-session-at", "--rewind-files",
		"--resume-drops-turn")
	variadicFlags = set("--add-dir", "--allowedTools", "--allowed-tools", "--disallowedTools",
		"--disallowed-tools", "--betas", "--file", "--mcp-config", "--tools",
		"--channels", "--dangerously-load-development-channels")
	optionalValueFlags = set("-r", "--resume", "-d", "--debug", "--from-pr", "--prompt-suggestions",
		"--remote-control", "--rc")
	booleanFlags = set("--allow-dangerously-skip-permissions", "--ax-screen-reader", "--brief", "--chrome",
		"-c", "--continue", "--dangerously-skip-permissions", "--disable-slash-commands",
		"--exclude-dynamic-system-prompt-sections", "--fork-session", "--forward-subagent-text", "--ide",
		"--include-hook-events", "--include-partial-messages", "--no-chrome", "--replay-user-messages",
		"--restricted", "--strict-mcp-config", "--verbose",
		// hidden
		"--enable-auto-mode", "--reply-on-resume", "--init", "--init-only",
		"--maintenance", "--plan-mode-required", "--allow-other-models", "--include-unsupported-models",
		"--debug-to-stderr", "-d2e")
	// Only meaningful for the first launch: which session to open, start-time
	// downloads, the turn to rewind to, and the starting permission mode
	// (relaunches use the mode the session last had, from its transcript).
	firstOnlyFlags = set("-r", "--resume", "-c", "--continue", "--session-id", "--fork-session", "--from-pr",
		"--file", "--resume-session-at", "--rewind-files", "--resume-drops-turn", "--permission-mode")
	// Modes baton cannot supervise: no hooks (--bare, --safe-mode), not an
	// interactive local session, or a cwd baton does not control.
	refusedFlags = map[string]string{
		"-p": "print mode is not supervised", "--print": "print mode is not supervised",
		"--settings":  "baton passes its own --settings; put your settings in the profile instead",
		"--bare":      "--bare skips hooks, so baton could not see usage limits",
		"--safe-mode": "--safe-mode disables hooks, so baton could not see usage limits",
		"--bg":        "background sessions are not supervised", "--background": "background sessions are not supervised",
		"--cloud": "cloud sessions are not supervised", "--remote": "cloud sessions are not supervised",
		"--environment": "cloud sessions are not supervised", "--pool": "cloud sessions are not supervised",
		"--attach-serve": "remote sessions are not supervised", "--sdk-url": "remote sessions are not supervised",
		"--desktop": "desktop sessions are not supervised", "--teleport": "teleport sessions are not supervised",
		"-w": "worktree sessions are not supported yet", "--worktree": "worktree sessions are not supported yet",
		"--tmux":                   "tmux sessions are not supported",
		"--no-session-persistence": "the session must be saved to continue on another account",
		"-h":                       "run `claude --help` directly", "--help": "run `claude --help` directly",
		"-v": "run `claude --version` directly", "--version": "run `claude --version` directly",
	}
	// Short flags that take no value, for expanding clusters like "-cd".
	shortBooleans = set("-c", "-p", "-h", "-v")
)

func set(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func known(name string) bool {
	return valueFlags[name] || variadicFlags[name] || optionalValueFlags[name] || booleanFlags[name]
}

// SplitArgs classifies the claude arguments given after `baton run --`.
func SplitArgs(in []string) (Args, error) {
	var a Args
	toks := append([]string{}, in...)
	var tail []string // everything after a literal "--": always prompt text
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok == "--" {
			tail = append([]string{"--"}, toks[i+1:]...)
			break
		}
		if !strings.HasPrefix(tok, "-") || tok == "-" {
			a.FirstOnly = append(a.FirstOnly, tok) // positional prompt
			a.Original = append(a.Original, tok)
			continue
		}
		name, value, hasInline := strings.Cut(tok, "=")
		if hasInline && booleanFlags[name] {
			return Args{}, fmt.Errorf("%s does not take a value (claude would refuse %q)", name, tok)
		}
		// Commander-style short clusters: "-rabc" is -r with value "abc",
		// "-cd" is -c followed by -d. A whole known token (like -d2e) is not split.
		if !strings.HasPrefix(tok, "--") && len(tok) > 2 && !hasInline && !known(tok) {
			short := tok[:2]
			switch {
			case valueFlags[short] || optionalValueFlags[short]:
				name, value, hasInline = short, tok[2:], true
			case shortBooleans[short] || refusedFlags[short] != "":
				toks = append(toks[:i+1], append([]string{"-" + tok[2:]}, toks[i+1:]...)...)
				name, tok = short, short
			}
		}
		if why, bad := refusedFlags[name]; bad {
			return Args{}, fmt.Errorf("%s: %s", name, why)
		}
		group := []string{name}
		if hasInline {
			group = []string{name, value}
		} else {
			switch {
			case valueFlags[name]:
				if i+1 >= len(toks) {
					return Args{}, fmt.Errorf("%s needs a value", name)
				}
				i++
				value = toks[i]
				group = append(group, value)
			case variadicFlags[name]:
				for i+1 < len(toks) && !strings.HasPrefix(toks[i+1], "-") {
					i++
					group = append(group, toks[i])
				}
			case optionalValueFlags[name]:
				if i+1 < len(toks) && !strings.HasPrefix(toks[i+1], "-") {
					i++
					value = toks[i]
					group = append(group, value)
				}
			case !known(name):
				if i+1 < len(toks) && !strings.HasPrefix(toks[i+1], "-") {
					return Args{}, fmt.Errorf("baton does not know %s. If it takes a value, write %s=<value>; "+
						"if not, put your prompt before the options", name, name)
				}
			}
		}
		if hasInline && !known(name) {
			group = []string{tok} // unknown --flag=value: pass through untouched
		}
		switch name {
		case "-r", "--resume":
			a.Resumes, a.ResumeArg = true, value
		case "-c", "--continue":
			a.Resumes, a.Continue = true, true
		case "--from-pr":
			a.Resumes = true
		}
		a.Original = append(a.Original, group...)
		if firstOnlyFlags[name] {
			a.FirstOnly = append(a.FirstOnly, group...)
			continue
		}
		a.Persistent = append(a.Persistent, group...)
	}
	// Keep "--" last so nothing baton appends ever lands after it.
	a.FirstOnly = append(a.FirstOnly, tail...)
	a.Original = append(a.Original, tail...)
	return a, nil
}

// HasFlag reports whether args contain the flag (bare or --flag=value).
func HasFlag(args []string, names ...string) bool {
	for _, t := range args {
		n, _, _ := strings.Cut(t, "=")
		for _, want := range names {
			if n == want {
				return true
			}
		}
	}
	return false
}

// ReplaceResume rewrites the -r/--resume value (or replaces -c/--continue)
// with a transcript path, keeping every other argument where it was.
func (a *Args) ReplaceResume(path string) {
	a.FirstOnly = replaceResume(a.FirstOnly, a.ResumeArg, path)
	a.Original = replaceResume(a.Original, a.ResumeArg, path)
	a.ResumeArg = path
	a.Continue = false
}

func replaceResume(toks []string, oldValue, path string) []string {
	out := make([]string, 0, len(toks)+1)
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok == "--" {
			return append(out, toks[i:]...)
		}
		name, _, hasInline := strings.Cut(tok, "=")
		switch name {
		case "-r", "--resume":
			if !hasInline && oldValue != "" && i+1 < len(toks) && toks[i+1] == oldValue {
				i++
			}
			out = append(out, "--resume", path)
			return append(out, toks[i+1:]...)
		case "-c", "--continue":
			out = append(out, "--resume", path)
			return append(out, toks[i+1:]...)
		}
		out = append(out, tok)
	}
	return out
}
