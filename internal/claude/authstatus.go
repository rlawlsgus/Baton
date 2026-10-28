package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// AuthStatus is the non-secret part of `claude auth status --json`.
type AuthStatus struct {
	LoggedIn        bool
	AuthMethod      string // "claude.ai" for a subscription login
	APIProvider     string // "firstParty" unless a cloud provider overrides it
	Email           string
	AccountUUID     string
	OrgID           string
	OrgName         string
	Subscription    string
	ConfigDirectory string
}

// ReadAuthStatus asks claude.exe about a profile's login. baton never opens
// the profile's credential store itself.
func ReadAuthStatus(exe string, env []string) (AuthStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "auth", "status", "--json")
	cmd.Env = env
	cmd.SysProcAttr = hidden()
	cmd.Stdin = nil
	cmd.Stderr = nil
	out, err := cmd.Output()
	// Exit code 1 means "not logged in" but still prints JSON.
	var raw map[string]any
	if jerr := json.Unmarshal(out, &raw); jerr != nil {
		if err != nil {
			return AuthStatus{}, fmt.Errorf("claude auth status: %w", err)
		}
		return AuthStatus{}, fmt.Errorf("claude auth status: unexpected output: %w", jerr)
	}
	return parseAuthStatus(raw), nil
}

func parseAuthStatus(raw map[string]any) AuthStatus {
	s := AuthStatus{
		LoggedIn:        boolField(raw, "loggedIn"),
		AuthMethod:      strField(raw, "authMethod"),
		APIProvider:     strField(raw, "apiProvider"),
		Email:           strField(raw, "email", "emailAddress", "accountEmail"),
		AccountUUID:     strField(raw, "accountUuid", "accountId", "accountUUID"),
		OrgID:           strField(raw, "orgId", "organizationUuid", "organizationId", "orgUuid"),
		OrgName:         strField(raw, "orgName", "organizationName"),
		Subscription:    strField(raw, "subscriptionType", "subscription", "plan"),
		ConfigDirectory: strField(raw, "configDirectory"),
	}
	for _, nested := range []string{"account", "oauthAccount"} {
		acct, ok := raw[nested].(map[string]any)
		if !ok {
			continue
		}
		if s.Email == "" {
			s.Email = strField(acct, "email", "emailAddress")
		}
		if s.AccountUUID == "" {
			s.AccountUUID = strField(acct, "accountUuid", "uuid", "id")
		}
		if s.OrgID == "" {
			s.OrgID = strField(acct, "organizationUuid", "orgId", "organizationId")
		}
		if s.OrgName == "" {
			s.OrgName = strField(acct, "orgName", "organizationName")
		}
	}
	return s
}

func strField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func boolField(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

// RunInteractive runs claude.exe with the console attached (for `auth login`
// and similar flows the user completes themselves) and returns its exit code.
func RunInteractive(exe string, args []string, env []string, dir string) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}
