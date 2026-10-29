package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// FolderRule keeps the sessions in a folder, and in the folders under it, on
// some of the user's accounts: they start and switch only among these. One
// account means the session never moves (a company repository that must
// stay on the company account, say).
type FolderRule struct {
	Folder   string   `json:"folder"`   // a local folder, absolute
	Accounts []string `json:"accounts"` // the accounts it may use, at least one
}

// Allows reports whether a session under the rule may use the account.
func (r *FolderRule) Allows(id string) bool { return r == nil || slices.Contains(r.Accounts, id) }

var driveRx = regexp.MustCompile(`^[A-Za-z]:\\`)

// cleanFolder is a folder as rules compare it: quotes (Explorer's "Copy as
// path") and a trailing separator dropped.
func cleanFolder(p string) string { return filepath.Clean(strings.Trim(strings.TrimSpace(p), `"`)) }

// within reports whether dir is folder or inside it (Windows paths: case
// does not matter).
func within(dir, folder string) bool {
	d, f := strings.ToLower(cleanFolder(dir)), strings.ToLower(cleanFolder(folder))
	if d == f {
		return true
	}
	if !strings.HasSuffix(f, `\`) {
		f += `\`
	}
	return strings.HasPrefix(d, f)
}

// RuleFor returns the rule that applies to dir: the one for the closest
// folder that is dir or holds it. nil means any account may be used.
func (c Config) RuleFor(dir string) *FolderRule {
	var best *FolderRule
	for i := range c.FolderRules {
		r := &c.FolderRules[i]
		if within(dir, r.Folder) && (best == nil || len(cleanFolder(r.Folder)) > len(cleanFolder(best.Folder))) {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	cp := FolderRule{Folder: best.Folder, Accounts: slices.Clone(best.Accounts)}
	return &cp
}

// SetRule adds the rule for folder, or replaces the one there is.
func (c *Config) SetRule(folder string, accounts []string) error {
	r := FolderRule{Folder: cleanFolder(folder), Accounts: dedupe(accounts)}
	if err := r.validate(); err != nil {
		return err
	}
	for i := range c.FolderRules {
		if strings.EqualFold(cleanFolder(c.FolderRules[i].Folder), r.Folder) {
			c.FolderRules[i] = r
			return nil
		}
	}
	c.FolderRules = append(c.FolderRules, r)
	return nil
}

// RemoveRule drops the rule for exactly folder; it reports whether there was one.
func (c *Config) RemoveRule(folder string) bool {
	for i := range c.FolderRules {
		if strings.EqualFold(cleanFolder(c.FolderRules[i].Folder), cleanFolder(folder)) {
			c.FolderRules = slices.Delete(c.FolderRules, i, i+1)
			return true
		}
	}
	return false
}

func (r FolderRule) validate() error {
	switch {
	case !driveRx.MatchString(r.Folder):
		return fmt.Errorf("folder rule: %q is not a local folder (like C:\\work)", r.Folder)
	case len(r.Accounts) == 0:
		return errors.New("folder rule: name at least one account")
	}
	for _, a := range r.Accounts {
		if strings.TrimSpace(a) == "" {
			return errors.New("folder rule: an account name is empty")
		}
	}
	return nil
}

func validateRules(rules []FolderRule) error {
	seen := map[string]bool{}
	for _, r := range rules {
		if err := r.validate(); err != nil {
			return fmt.Errorf("config: %w", err)
		}
		k := strings.ToLower(cleanFolder(r.Folder))
		if seen[k] {
			return fmt.Errorf("config: two folder rules for %s", r.Folder)
		}
		seen[k] = true
	}
	return nil
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Stricter reports whether rule next keeps a session at least as close as
// cur does: a running session only ever takes on a stricter rule, as with
// the switch mode, so nothing running inside it can loosen its own.
func Stricter(next, cur *FolderRule) bool {
	if next == nil {
		return cur == nil
	}
	if cur == nil {
		return true
	}
	for _, a := range next.Accounts {
		if !slices.Contains(cur.Accounts, a) {
			return false
		}
	}
	return true
}
