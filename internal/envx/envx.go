// Package envx builds the environment for claude.exe child processes.
//
// The parent environment is kept (PATH, proxies, CA bundles, user tweaks) except
// for variables that would override the selected account's login, route
// requests to another provider, or leak a host session's identity (for example
// when baton is started from inside another Claude Code session).
package envx

import (
	"os"
	"sort"
	"strings"
)

var scrubExact = map[string]bool{
	// Credentials and providers that outrank the profile's /login.
	"ANTHROPIC_API_KEY":                         true,
	"ANTHROPIC_AUTH_TOKEN":                      true,
	"ANTHROPIC_BASE_URL":                        true,
	"CLAUDE_CODE_USE_BEDROCK":                   true,
	"CLAUDE_CODE_USE_VERTEX":                    true,
	"CLAUDE_CODE_USE_FOUNDRY":                   true,
	"CLAUDE_CODE_USE_ANTHROPIC_AWS":             true,
	"CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD":    true,
	"CLAUDE_CODE_USE_MANTLE":                    true,
	"CLAUDE_CODE_USE_GATEWAY":                   true,
	"ANTHROPIC_PROFILE":                         true,
	"ANTHROPIC_CONFIG_DIR":                      true,
	"ANTHROPIC_FEDERATION_RULE_ID":              true,
	"ANTHROPIC_ORGANIZATION_ID":                 true,
	"ANTHROPIC_WORKSPACE_ID":                    true,
	"ANTHROPIC_IDENTITY_TOKEN":                  true,
	"ANTHROPIC_IDENTITY_TOKEN_FILE":             true,
	"ANTHROPIC_AWS_API_KEY":                     true,
	"CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR":       true,
	"CLAUDE_CODE_SIMPLE":                        true, // what --bare sets: no hooks
	"CLAUDE_CODE_SAFE_MODE":                     true, // what --safe-mode sets: no hooks
	"CLAUDE_CONFIG_DIR":                         true, // set by baton per account
	"CLAUDE_SECURESTORAGE_CONFIG_DIR":           true,
	"CLAUDE_CODE_ORGANIZATION_UUID":             true,
	"CLAUDE_CODE_ACCOUNT_UUID":                  true,
	"CLAUDE_CODE_USER_EMAIL":                    true,
	"CLAUDE_CODE_SESSION_ACCESS_TOKEN":          true,
	"CCR_OAUTH_TOKEN_FILE":                      true,
	"CLAUDE_CODE_SDK_HAS_HOST_AUTH_REFRESH":     true,
	"CLAUDE_CODE_ENTRYPOINT":                    true,
	"CLAUDE_CODE_SESSION_ID":                    true,
	"CLAUDE_CODE_CHILD_SESSION":                 true,
	"CLAUDE_CODE_SESSION_ATTENDED":              true,
	"CLAUDE_CODE_EXECPATH":                      true,
	"CLAUDE_CODE_DESKTOP_APP_VERSION":           true,
	"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": true,
	"CLAUDE_CODE_TERMINAL_MCP_TOOLS":            true,
	"CLAUDECODE":                                true,
	"CLAUDE_PID":                                true,
	"CLAUDE_ENV_FILE":                           true,
	"CLAUDE_PROJECT_DIR":                        true,
	"BATON_RUN":                                 true,
	"BATON_NEW_CONSOLE":                         true, // must never reach `baton hook`
	"BATON_SIGNIN_PRIVATE":                      true, // only a sign-in's own browser command
}

var scrubPrefixes = []string{
	"CLAUDE_CODE_OAUTH",      // CLAUDE_CODE_OAUTH_TOKEN(_FILE_DESCRIPTOR), _REFRESH_TOKEN, _SCOPES
	"CLAUDE_CODE_HOST_",      // desktop-app host auth/session
	"CLAUDE_CODE_MESSAGING_", // host messaging socket + token
	"CLAUDE_AGENT_SDK_",
	"CLAUDE_CODE_SDK_",
}

// Scrubbed reports whether a variable name is removed from child envs.
func Scrubbed(name string) bool {
	up := strings.ToUpper(name)
	if scrubExact[up] {
		return true
	}
	for _, p := range scrubPrefixes {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}

// Build returns base (KEY=VALUE pairs) minus scrubbed names, plus set.
// It also returns the names that were removed, so callers can tell the user.
func Build(base []string, set map[string]string) (env []string, removed []string) {
	overridden := map[string]bool{} // Windows env names are case-insensitive
	for k := range set {
		overridden[strings.ToUpper(k)] = true
	}
	for _, kv := range base {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || name == "" {
			// Windows keeps per-drive cwd entries like "=C:=C:\x"; pass them through.
			env = append(env, kv)
			continue
		}
		if Scrubbed(name) {
			removed = append(removed, name)
			continue
		}
		if overridden[strings.ToUpper(name)] {
			continue
		}
		env = append(env, kv)
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+set[k])
	}
	sort.Strings(removed)
	return env, removed
}

// ForProfile is the env for claude.exe running on one account profile.
func ForProfile(profileDir string, extra map[string]string) (env []string, removed []string) {
	set := map[string]string{"CLAUDE_CONFIG_DIR": profileDir}
	for k, v := range extra {
		set[k] = v
	}
	return Build(os.Environ(), set)
}

// Notable filters removed names down to the ones worth telling the user about:
// things they probably set on purpose, not host-session plumbing.
func Notable(removed []string) []string {
	var out []string
	for _, n := range removed {
		switch strings.ToUpper(n) {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL",
			"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
			"CLAUDE_CODE_USE_ANTHROPIC_AWS", "CLAUDE_CODE_USE_ANTHROPIC_GOOGLE_CLOUD",
			"CLAUDE_CODE_USE_MANTLE", "CLAUDE_CODE_USE_GATEWAY", "ANTHROPIC_PROFILE",
			"CLAUDE_CODE_SIMPLE", "CLAUDE_CODE_SAFE_MODE",
			"CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN":
			out = append(out, n)
		}
	}
	return out
}
