package main

import (
	"os"
	"path/filepath"
	"testing"

	"baton/internal/accounts"
	"baton/internal/claude"
)

// `claude auth status` has no account id; it comes from the profile's own
// .claude.json, when that describes the sign-in the status reports (same
// organization and email). A signed-out profile keeps the ids it had, and
// duplicates are told apart by the id once both have one.
func TestApplyAuthAccountIDs(t *testing.T) {
	const (
		id   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		org1 = "a0000000-0000-4000-8000-000000000001"
		org2 = "a0000000-0000-4000-8000-000000000002"
	)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"`+id+
		`","organizationUuid":"`+org1+`","emailAddress":"me@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	signedIn := func(org, email, method string) accounts.Account {
		var a accounts.Account
		applyAuth(&a, claude.AuthStatus{LoggedIn: true, AuthMethod: method, Email: email, OrgID: org}, dir)
		return a
	}
	a := signedIn(org1, "Me@Example.com", "claude.ai")
	if a.AccountUUID != id || a.OrgID != org1 || !a.LoggedIn {
		t.Fatalf("signed in: %+v", a)
	}
	// The file is from another sign-in (another organization or person).
	for _, b := range []accounts.Account{signedIn(org2, "me@example.com", "claude.ai"), signedIn(org1, "you@example.com", "claude.ai")} {
		if b.AccountUUID != "" {
			t.Fatalf("an id from another sign-in: %+v", b)
		}
	}
	if k := signedIn("", "", "api_key"); k.AccountUUID != "" || k.OrgID != "" {
		t.Fatalf("an API key: %+v", k)
	}
	// Signed out: the ids stay (the desktop app signs in on its own).
	out := a
	applyAuth(&out, claude.AuthStatus{AuthMethod: "none"}, dir)
	if out.LoggedIn || out.AccountUUID != id || out.OrgID != org1 || out.Email != "" {
		t.Fatalf("signed out: %+v", out)
	}
	// Signing in as someone else in that profile replaces them.
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",`+
		`"organizationUuid":"`+org2+`","emailAddress":"you@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	applyAuth(&out, claude.AuthStatus{LoggedIn: true, AuthMethod: "claude.ai", Email: "you@example.com", OrgID: org2}, dir)
	if out.AccountUUID != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" || out.OrgID != org2 {
		t.Fatalf("signed in as someone else: %+v", out)
	}

	// Duplicates: the id decides once both have one; without, the email.
	x := accounts.Account{AccountUUID: id, OrgID: org1, Email: "me@example.com"}
	renamed := accounts.Account{AccountUUID: id, OrgID: org1, Email: "new@example.com"}
	other := accounts.Account{AccountUUID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", OrgID: org1, Email: "me@example.com"}
	older := accounts.Account{OrgID: org1, Email: "ME@example.com"}
	team := accounts.Account{AccountUUID: id, OrgID: org2, Email: "me@example.com"}
	if !accounts.SameIdentity(x, renamed) || accounts.SameIdentity(x, other) || !accounts.SameIdentity(x, older) || accounts.SameIdentity(x, team) {
		t.Fatal("SameIdentity")
	}
}
