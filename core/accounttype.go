// core/accounttype.go
package core

// AccountType classifies a profile's logged-in Claude account for sync purposes.
// Team accounts serve their Code sidebar from an Anthropic server API, so sessions
// copied into a Team profile locally never appear (import is a no-op).
type AccountType int

const (
	AccountUnknown AccountType = iota
	AccountPersonal
	AccountTeam
)

func (t AccountType) String() string {
	switch t {
	case AccountPersonal:
		return "Personal"
	case AccountTeam:
		return "Team"
	default:
		return "Unknown"
	}
}

// orgInfo is one organization the account belongs to, as read from the account
// bootstrap payload cached in Local Storage.
type orgInfo struct {
	Name    string
	Tier    string
	Billing string
}

// teamTiers/personalTiers are explicit allow-lists of Anthropic rate-limit tier
// codenames. "raven" is the internal codename for the Team/Enterprise product.
// Unrecognized tiers deliberately fall through to Unknown (graceful under-warn).
var teamTiers = map[string]bool{
	"default_raven": true,
}

var personalTiers = map[string]bool{
	"default_claude_ai":      true,
	"default_claude_pro":     true,
	"default_claude_max":     true,
	"default_claude_max_5x":  true,
	"default_claude_max_20x": true,
	"auto_api_evaluation":    true,
}

// classify returns Team if any org is on a team tier; Personal if the (non-empty)
// list is entirely personal tiers; Unknown otherwise (empty list or any tier in
// neither allow-list).
func classify(orgs []orgInfo) AccountType {
	if len(orgs) == 0 {
		return AccountUnknown
	}
	allPersonal := true
	for _, o := range orgs {
		if teamTiers[o.Tier] {
			return AccountTeam
		}
		if !personalTiers[o.Tier] {
			allPersonal = false
		}
	}
	if allPersonal {
		return AccountPersonal
	}
	return AccountUnknown
}
