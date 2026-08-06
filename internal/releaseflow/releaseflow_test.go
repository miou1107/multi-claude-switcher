// Package releaseflow holds no code. It exists for the test below, which reads
// the release workflow and fails when more than one job can create the GitHub
// release.
//
// Two jobs did. build-macos and build-windows both ran
// softprops/action-gh-release, which creates the release if it is missing, and
// they run at the same time. Measured on v0.13.3: both created one, the Windows
// job's won, the macOS job's was left as an unpublished draft holding the only
// copy of the mac download, and its attempt to finalize failed with
// "already_exists". The published release carried the Windows installer alone,
// and update-homebrew-tap was skipped because the job it needs had failed. Four
// earlier tags had raced the same way and won.
//
// Nothing about that is visible by reading either job on its own, which is why
// it survived four releases. The rule this test enforces is the fix: exactly
// one job creates the release, everything else only uploads into it.
package releaseflow

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const workflow = "../../.github/workflows/release.yml"

// createsARelease matches the two ways this workflow can bring a release into
// existence: `gh release create`, and softprops/action-gh-release, which
// creates one when the tag has none. `gh release upload`, which the build jobs
// use, cannot — it fails if the release is missing rather than making one.
//
// Comment lines are skipped before matching. Without that, the first version of
// this test passed by matching the comment in the workflow that explains why
// the rule exists — the workflow had zero creating steps at the time.
var createsARelease = regexp.MustCompile(`gh release create|uses:\s*softprops/action-gh-release`)

func TestOnlyOneJobCreatesTheRelease(t *testing.T) {
	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("reading the release workflow: %v", err)
	}

	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		n += len(createsARelease.FindAllString(line, -1))
	}
	if n != 1 {
		t.Errorf("%d steps in the release workflow can create the release, want exactly 1: "+
			"concurrent jobs each make their own, one wins, and the loser's assets are "+
			"stranded in a draft nobody sees", n)
	}
}

// TestEveryUploadNamesTheTag guards the other half. `gh release upload` with no
// tag argument uploads nowhere useful, and with the wrong one it publishes into
// a release from an earlier version — neither fails the job.
func TestEveryUploadNamesTheTag(t *testing.T) {
	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("reading the release workflow: %v", err)
	}

	uploads := 0
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "gh release upload") {
			continue
		}
		uploads++
		if !strings.Contains(line, "$GITHUB_REF_NAME") && !strings.Contains(line, "github.ref_name") {
			t.Errorf("an upload does not name the tag it is uploading to: %s", strings.TrimSpace(line))
		}
	}
	// The two platform builds. Zero would mean this test is reading a workflow
	// that no longer uploads anything the way it thinks.
	if uploads != 2 {
		t.Errorf("found %d upload steps, want 2 (macOS and Windows)", uploads)
	}
}
