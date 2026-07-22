package main

import "testing"

func TestHasSkipInstanceFlag(t *testing.T) {
	if !hasSkipInstanceFlag([]string{"/path/mcs-tray", relaunchSkipInstanceCheckFlag}) {
		t.Error("expected true when the flag is present")
	}
	if hasSkipInstanceFlag([]string{"/path/mcs-tray"}) {
		t.Error("expected false when the flag is absent")
	}
}

func TestOtherTrayRunning(t *testing.T) {
	const self = 501
	cases := []struct {
		name string
		ps   string
		want bool
	}{
		{
			name: "another mcs-tray with a different pid",
			ps:   "  501 /Applications/Multi-Claude Switcher.app/Contents/MacOS/mcs-tray\n  777 /Users/x/mcs-tray --mcs-relaunch\n",
			want: true,
		},
		{
			name: "only our own mcs-tray",
			ps:   "  501 /Applications/Multi-Claude Switcher.app/Contents/MacOS/mcs-tray\n  888 ps -axo pid=,command=\n",
			want: false,
		},
		{
			name: "no mcs-tray at all",
			ps:   "  501 /bin/zsh\n  888 /Applications/Claude.app/Contents/MacOS/Claude\n",
			want: false,
		},
	}
	for _, c := range cases {
		if got := otherTrayRunning(c.ps, self); got != c.want {
			t.Errorf("%s: otherTrayRunning=%v want %v", c.name, got, c.want)
		}
	}
}
