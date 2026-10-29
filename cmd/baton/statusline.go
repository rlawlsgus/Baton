package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"baton/internal/accounts"
	"baton/internal/app"
	"baton/internal/usage"
)

// cmdStatusLine is the status line of a baton session: Claude Code runs it
// with a JSON payload on stdin and shows what it prints under the prompt. It
// shows the account the session runs on and its plan usage, and records the
// usage for the baton window. It must be quick and never fail loudly.
func cmdStatusLine() {
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	account := ""
	var u usage.Usage
	ok := false
	if p, err := app.DefaultPaths(); err == nil {
		// The account is the one whose profile this claude runs in.
		if reg, err := accounts.Load(p.Accounts()); err == nil {
			account = accountForProfile(reg, os.Getenv("CLAUDE_CONFIG_DIR"))
		}
		if u, ok = usage.FromStatusLine(data, time.Now()); ok && account != "" {
			_ = usage.Save(p.UsageDir(), account, u)
		}
		if !ok && account != "" {
			// Not reported yet in this claude (it comes with a reply): the
			// last known, if still current.
			if last, found := usage.Load(p.UsageDir(), account); found && time.Since(last.Updated) < 6*time.Hour {
				u, ok = last.Current(time.Now()), true
			}
		}
	}
	fmt.Println(statusText(account, u, ok))
}

func accountForProfile(reg accounts.Registry, profileDir string) string {
	if profileDir == "" {
		return ""
	}
	for _, a := range reg.Accounts {
		if strings.EqualFold(filepath.Clean(a.ProfileDir), filepath.Clean(profileDir)) {
			return a.ID
		}
	}
	return ""
}

// statusText is the line under Claude Code's prompt, e.g.
// "baton · work · 5h 34% · week 12%".
func statusText(account string, u usage.Usage, ok bool) string {
	parts := []string{"baton"}
	if account != "" {
		parts = append(parts, account)
	}
	if ok {
		if w := u.FiveHour; w != nil {
			parts = append(parts, fmt.Sprintf(say("5h %d%%", "5시간 %d%%"), int(math.Round(w.Used))))
		}
		if w := u.SevenDay; w != nil {
			parts = append(parts, fmt.Sprintf(say("week %d%%", "주간 %d%%"), int(math.Round(w.Used))))
		}
	}
	return strings.Join(parts, " · ")
}
