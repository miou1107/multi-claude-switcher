//go:build windows

package main

import "testing"

func TestOtherTrayInTasklist(t *testing.T) {
	const self = 501
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "another mcs-tray with a different pid",
			output: "\"mcs-tray.exe\",\"777\",\"Console\",\"1\",\"10,000 K\"\n",
			want:   true,
		},
		{
			name:   "only our own mcs-tray",
			output: "\"mcs-tray.exe\",\"501\",\"Console\",\"1\",\"10,000 K\"\n",
			want:   false,
		},
		{
			name:   "no tasks (info line, no leading quote)",
			output: "INFO: No tasks are running which match the specified criteria.\n",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
		{
			name:   "self plus another",
			output: "\"mcs-tray.exe\",\"501\",\"Console\",\"1\",\"10,000 K\"\n\"mcs-tray.exe\",\"902\",\"Console\",\"1\",\"9,000 K\"\n",
			want:   true,
		},
	}
	for _, c := range cases {
		if got := otherTrayInTasklist(c.output, self); got != c.want {
			t.Errorf("%s: otherTrayInTasklist=%v want %v", c.name, got, c.want)
		}
	}
}
