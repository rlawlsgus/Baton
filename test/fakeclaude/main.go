// Command fakeclaude stands in for claude.exe in baton's CLI tests. It only
// implements what account management calls: --version and auth
// login/status/logout. The "login" is a file in the profile, so tests can sign
// accounts in without a browser or a real account. As Claude Code does, a
// sign-in also notes the account in the profile's .claude.json
// (oauthAccount), with ids made from the email address.
//
//	FAKE_EMAIL       email the next `auth login` signs in as (default: <profile name>@example.com)
//	FAKE_LOGIN_FAIL  make `auth login` fail (exit 1) and leave any old login in place
//	FAKE_OLD_CLI     answer --version as an old CLI when run from a .local folder
//	FAKE_BROWSER_URL have `auth login` open this address with the BROWSER
//	                 command, as Claude Code opens its sign-in page
//
// A version written next to the exe (claude.exe.ver) is the one --version
// answers; `update` changes it to the one in claude.exe.update, as
// `claude update` does for a CLI.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func main() {
	args := os.Args[1:]
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	state := filepath.Join(cfg, "fake-auth.json")
	switch {
	case len(args) == 1 && args[0] == "--version":
		if os.Getenv("FAKE_OLD_CLI") != "" && strings.Contains(strings.ToLower(os.Args[0]), ".local") {
			fmt.Println("2.0.9 (Claude Code)") // a CLI installed long ago
			return
		}
		if v := sidecar(".ver"); v != "" {
			fmt.Println(v + " (Claude Code)")
			return
		}
		fmt.Println("2.1.308 (Claude Code)")
	case len(args) == 1 && args[0] == "update":
		to := sidecar(".update")
		if to == "" {
			fmt.Println("Claude Code is up to date")
			return
		}
		self, _ := os.Executable()
		if err := os.WriteFile(self+".ver", []byte(to), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Successfully updated to version " + to)
	case len(args) >= 2 && args[0] == "auth" && args[1] == "login":
		if u, browser := os.Getenv("FAKE_BROWSER_URL"), os.Getenv("BROWSER"); u != "" && browser != "" {
			// Claude Code splits BROWSER like a command line and adds the address;
			// the sign-in goes on whatever the browser command does.
			if words, err := windows.DecomposeCommandLine(browser); err == nil && len(words) > 0 {
				cmd := exec.Command(words[0], append(words[1:], u)...)
				cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
				_ = cmd.Run()
			}
		}
		if os.Getenv("FAKE_LOGIN_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "OAuth error: invalid code")
			os.Exit(1)
		}
		email := os.Getenv("FAKE_EMAIL")
		if email == "" {
			email = filepath.Base(cfg) + "@example.com"
		}
		org := fakeID("org:" + email)
		b, _ := json.Marshal(map[string]any{"loggedIn": true, "authMethod": "claude.ai", "apiProvider": "firstParty",
			"email": email, "orgId": org, "orgName": email + "'s Organization", "subscriptionType": "max", "configDirectory": cfg})
		if err := os.WriteFile(state, b, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if cfg != "" {
			m := map[string]any{}
			if old, err := os.ReadFile(filepath.Join(cfg, ".claude.json")); err == nil {
				_ = json.Unmarshal(old, &m)
			}
			m["oauthAccount"] = map[string]any{"accountUuid": fakeID("account:" + email), "organizationUuid": org, "emailAddress": email}
			b, _ = json.Marshal(m)
			_ = os.WriteFile(filepath.Join(cfg, ".claude.json"), b, 0o600)
		}
		fmt.Println("Login successful.")
	case len(args) >= 2 && args[0] == "auth" && args[1] == "status":
		b, err := os.ReadFile(state)
		if err != nil {
			b, _ = json.Marshal(map[string]any{"loggedIn": false, "authMethod": "none", "apiProvider": "firstParty", "configDirectory": cfg})
			fmt.Println(string(b))
			os.Exit(1)
		}
		fmt.Println(string(b))
	case len(args) >= 2 && args[0] == "auth" && args[1] == "logout":
		os.Remove(state)
		fmt.Println("Successfully logged out.")
	default:
		fmt.Fprintf(os.Stderr, "fakeclaude: unsupported %q\n", args)
		os.Exit(2)
	}
}

// fakeID is a uuid made from s, the same every time.
func fakeID(s string) string {
	h := sha256.Sum256([]byte(s))
	x := hex.EncodeToString(h[:16])
	return x[0:8] + "-" + x[8:12] + "-4" + x[13:16] + "-8" + x[17:20] + "-" + x[20:32]
}

// sidecar reads a file next to this exe (claude.exe<ext>), or "".
func sidecar(ext string) string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(self + ext)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
