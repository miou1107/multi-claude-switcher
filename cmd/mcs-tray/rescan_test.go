package main

import (
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestAnyComplete(t *testing.T) {
	tests := []struct {
		name     string
		accounts []core.ScannedAccount
		want     bool
	}{
		{
			name: "has a complete account",
			accounts: []core.ScannedAccount{
				{UUID: "ghost1", Complete: false},
				{UUID: "live1", Complete: true},
			},
			want: true,
		},
		{
			name: "all ghost accounts",
			accounts: []core.ScannedAccount{
				{UUID: "ghost1", Complete: false},
				{UUID: "ghost2", Complete: false},
			},
			want: false,
		},
		{
			name:     "empty slice",
			accounts: []core.ScannedAccount{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := anyComplete(tt.accounts); got != tt.want {
				t.Errorf("anyComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}
