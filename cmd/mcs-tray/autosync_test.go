package main

import "testing"

func TestShouldWarnAutoSync(t *testing.T) {
	cases := []struct{ enabling, dismissed, want bool }{
		{true, false, true},   // enabling, not dismissed -> warn
		{true, true, false},   // enabling, dismissed -> no warn
		{false, false, false}, // disabling -> never warn
		{false, true, false},
	}
	for _, c := range cases {
		if got := shouldWarnAutoSync(c.enabling, c.dismissed); got != c.want {
			t.Errorf("shouldWarnAutoSync(%v,%v)=%v want %v", c.enabling, c.dismissed, got, c.want)
		}
	}
}
