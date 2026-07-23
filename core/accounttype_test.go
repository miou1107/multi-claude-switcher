// core/accounttype_test.go
package core

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		orgs []orgInfo
		want AccountType
	}{
		{"team only", []orgInfo{{Name: "Fontrip", Tier: "default_raven"}}, AccountTeam},
		{"team plus personal", []orgInfo{
			{Name: "x's Organization", Tier: "default_claude_ai"},
			{Name: "Fontrip", Tier: "default_raven"},
		}, AccountTeam},
		{"personal max", []orgInfo{{Name: "x's Organization", Tier: "default_claude_max_20x"}}, AccountPersonal},
		{"personal mix", []orgInfo{
			{Tier: "default_claude_ai"}, {Tier: "auto_api_evaluation"},
		}, AccountPersonal},
		{"empty", nil, AccountUnknown},
		{"unknown tier", []orgInfo{{Tier: "default_claude_ai"}, {Tier: "default_mystery"}}, AccountUnknown},
	}
	for _, c := range cases {
		if got := classify(c.orgs); got != c.want {
			t.Errorf("%s: classify=%v want %v", c.name, got, c.want)
		}
	}
}
