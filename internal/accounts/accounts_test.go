package accounts

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestSameIdentity(t *testing.T) {
	cases := []struct {
		a, b Account
		want bool
	}{
		{Account{AccountUUID: "u1"}, Account{AccountUUID: "u1"}, true},
		{Account{AccountUUID: "u1", Email: "x@y"}, Account{AccountUUID: "u2", Email: "x@y"}, false},
		{Account{Email: "X@y"}, Account{Email: "x@y"}, true},
		{Account{Email: "x@y", OrgID: "o1"}, Account{Email: "x@y", OrgID: "o2"}, false},
		{Account{Email: "x@y", OrgID: "o1"}, Account{Email: "x@y"}, true},
		// One user, two organizations (personal plan + Team seat): two quotas.
		{Account{AccountUUID: "u1", OrgID: "o1"}, Account{AccountUUID: "u1", OrgID: "o2"}, false},
		{Account{Email: "x@y", OrgName: "Personal"}, Account{Email: "x@y", OrgName: "Acme"}, false},
		{Account{Email: "x@y", OrgName: "Acme"}, Account{Email: "X@y", OrgName: "acme"}, true},
		{Account{}, Account{}, false},
	}
	for i, c := range cases {
		if got := SameIdentity(c.a, c.b); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestValidateIDRejectsDeviceNames(t *testing.T) {
	for _, bad := range []string{"nul", "con", "com1", "lpt9", "Bad", "-x", ""} {
		if ValidateID(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, ok := range []string{"personal", "work-2", "a"} {
		if err := ValidateID(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
}

func TestUpdateDoesNotLoseConcurrentChanges(t *testing.T) {
	dir := t.TempDir()
	path, lock := filepath.Join(dir, "accounts.json"), filepath.Join(dir, "accounts.lock")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Update(path, lock, func(r *Registry) error {
				return r.Add(Account{ID: string(rune('a' + i)), Enabled: true})
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if r, _ := Load(path); len(r.Accounts) != 10 {
		t.Fatalf("lost updates: %d accounts", len(r.Accounts))
	}
}
