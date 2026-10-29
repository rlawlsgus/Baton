package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"baton/internal/accounts"
	"baton/internal/config"
)

const rulesUsage = "baton rules [set <folder> <account>[,<account>...] | remove <folder>]"

// cmdRules shows and changes the folder rules: which accounts the sessions
// in a folder, and in the folders under it, may start and switch on.
func cmdRules(args []string) error {
	e, err := load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if len(e.cfg.FolderRules) == 0 {
			fmt.Println("No folder rules: sessions in every folder may use every account.")
			fmt.Println("Add one with: " + rulesUsage)
			return nil
		}
		for _, r := range e.cfg.FolderRules {
			var missing []string
			for _, a := range r.Accounts {
				if e.reg.Get(a) == nil {
					missing = append(missing, a)
				}
			}
			line := r.Folder + "  " + strings.Join(r.Accounts, ", ")
			if len(missing) > 0 {
				line += "  (no such account: " + strings.Join(missing, ", ") + ")"
			}
			fmt.Println(line)
		}
		return nil
	}
	if args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stderr, "usage:", rulesUsage)
		return nil
	}
	if err := refuseInsideSession("change folder rules"); err != nil {
		return err
	}
	switch {
	case args[0] == "set" && len(args) == 3:
		folder, err := ruleFolder(args[1], true)
		if err != nil {
			return err
		}
		var accts []string
		for _, a := range strings.Split(args[2], ",") {
			if a = strings.TrimSpace(a); a == "" {
				continue
			}
			if e.reg.Get(a) == nil {
				return fmt.Errorf("no account named %q (see: baton list)", a)
			}
			accts = append(accts, a)
		}
		if _, err := config.Update(e.paths.Config(), e.paths.ConfigLock(), func(c *config.Config) error {
			return c.SetRule(folder, accts)
		}); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, say("Sessions in %s (and the folders in it) now use only %s.\n",
			"이제 %s 폴더(와 그 안의 폴더)의 세션은 %s 계정만 씁니다.\n"), folder, strings.Join(accts, ", "))
		return nil
	case args[0] == "remove" && len(args) == 2:
		folder, err := ruleFolder(args[1], false)
		if err != nil {
			return err
		}
		removed := false
		if _, err := config.Update(e.paths.Config(), e.paths.ConfigLock(), func(c *config.Config) error {
			removed = c.RemoveRule(folder)
			return nil
		}); err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("there is no rule for %s (see: baton rules)", folder)
		}
		fmt.Fprintf(os.Stderr, say("Removed the rule for %s.\n", "%s 폴더의 규칙을 지웠습니다.\n"), folder)
		return nil
	}
	return errors.New("usage: " + rulesUsage)
}

// ruleFolder is the absolute folder a rule is for; a new rule needs a
// folder that exists.
func ruleFolder(p string, mustExist bool) (string, error) {
	abs, err := filepath.Abs(strings.Trim(strings.TrimSpace(p), `"`))
	if err != nil {
		return "", err
	}
	if mustExist {
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return "", fmt.Errorf("%s is not a folder", abs)
		}
	}
	return abs, nil
}

// allowedOnly is the registry without the accounts rule does not allow.
func allowedOnly(reg accounts.Registry, rule *config.FolderRule) accounts.Registry {
	if rule == nil {
		return reg
	}
	out := accounts.Registry{Schema: reg.Schema}
	for _, a := range reg.Accounts {
		if rule.Allows(a.ID) {
			out.Accounts = append(out.Accounts, a)
		}
	}
	return out
}
