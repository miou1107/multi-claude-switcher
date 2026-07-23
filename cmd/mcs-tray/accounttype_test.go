package main

import (
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestProfileTitle(t *testing.T) {
	cases := []struct {
		name    string
		t       core.AccountType
		current bool
		want    string
	}{
		{"Company", core.AccountTeam, false, "Company  🏢 Team"},
		{"Company", core.AccountTeam, true, "Company  🏢 Team  (current)"},
		{"Personal", core.AccountPersonal, false, "Personal"},
		{"Personal", core.AccountPersonal, true, "Personal  (current)"},
		{"Mystery", core.AccountUnknown, false, "Mystery"},
	}
	for _, c := range cases {
		if got := profileTitle(c.name, c.t, c.current); got != c.want {
			t.Errorf("profileTitle(%q,%v,%v)=%q want %q", c.name, c.t, c.current, got, c.want)
		}
	}
}

func TestAcctTypeCache(t *testing.T) {
	setAcctType("/p/x", core.AccountTeam)
	if got := getAcctType("/p/x"); got != core.AccountTeam {
		t.Errorf("cache get = %v want Team", got)
	}
	if got := getAcctType("/p/unknown"); got != core.AccountUnknown {
		t.Errorf("cache miss = %v want Unknown", got)
	}
}
