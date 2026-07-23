package main

import (
	"testing"

	"github.com/miou1107/multi-claude-switcher/core"
)

func TestImportTargetIsTeam(t *testing.T) {
	setAcctType("/p/team", core.AccountTeam)
	setAcctType("/p/personal", core.AccountPersonal)
	if !importTargetIsTeam("/p/team") {
		t.Error("team target should be flagged")
	}
	if importTargetIsTeam("/p/personal") {
		t.Error("personal target should not be flagged")
	}
	if importTargetIsTeam("/p/unknown") {
		t.Error("unknown target should not be flagged (no false warning)")
	}
}
