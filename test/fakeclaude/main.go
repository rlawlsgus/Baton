// Command fakeclaude stands in for claude.exe in baton's CLI tests. It only
// implements what account management calls: --version and auth
// login/status/logout. The "login" is a file in the profile, so tests can sign
// accounts in without a browser or a real account.
//
//	FAKE_EMAIL       email the next `auth login` signs in as (default: <profile name>@example.com)
//	FAKE_LOGIN_FAIL  make `auth login` fail (exit 1) and leave any old login in place
//	FAKE_OLD_CLI     answer --version as an old CLI when run from a .local folder
//	FAKE_BROWSER_URL have `auth login` open this address with the BROWSER
//	                 command, as Claude Code opens its sign-in page
package main

import (
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
		fmt.Println("2.1.308 (Claude Code)")
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
		b, _ := json.Marshal(map[string]any{"loggedIn": true, "authMethod": "claude.ai", "apiProvider": "firstParty",
			"email": email, "orgName": email + "'s Organization", "subscriptionType": "max", "configDirectory": cfg})
		if err := os.WriteFile(state, b, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
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
