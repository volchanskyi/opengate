package main

import (
	"strings"
	"testing"
)

// TestCheckLinkPlanPolicy pins the plan-link doctrine:
//   - documentation under docs/ (other than ADRs) must not link ANY plan file,
//     archived or active — plans are ephemeral and get cleaned up;
//   - ADRs may link archived plans (a stable-enough target for a decision record);
//   - active-plan links are refused from every source;
//   - non-docs sources (README, plans, rules) keep the archived-plan allowance.
func TestCheckLinkPlanPolicy(t *testing.T) {
	const archivedTarget = ".claude/plans/archive/foo.md"

	cases := []struct {
		name        string
		source      string
		destination string
		wantSubstr  string // "" means the link must be accepted
	}{
		{
			name:        "non-ADR doc to archived plan is refused",
			source:      "docs/Testing.md",
			destination: "../.claude/plans/archive/foo.md",
			wantSubstr:  "documentation under docs/",
		},
		{
			name:        "non-ADR doc to active plan is refused",
			source:      "docs/Testing.md",
			destination: "../.claude/plans/foo.md",
			wantSubstr:  "documentation under docs/",
		},
		{
			name:        "ADR to archived plan is allowed",
			source:      "docs/adr/ADR-037-example.md",
			destination: "../../.claude/plans/archive/foo.md",
			wantSubstr:  "",
		},
		{
			name:        "ADR to active plan is refused",
			source:      "docs/adr/ADR-037-example.md",
			destination: "../../.claude/plans/foo.md",
			wantSubstr:  "active plan",
		},
		{
			name:        "non-docs source to archived plan is allowed",
			source:      "README.md",
			destination: ".claude/plans/archive/foo.md",
			wantSubstr:  "",
		},
		{
			name:        "non-docs source to active plan is refused",
			source:      "README.md",
			destination: ".claude/plans/foo.md",
			wantSubstr:  "active plan",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := newChecker(t.TempDir())
			if err != nil {
				t.Fatalf("newChecker: %v", err)
			}
			// Make the archived target resolvable so "allowed" cases reach a
			// clean result instead of a missing-target error.
			c.overlays[archivedTarget] = []byte("# archived plan\n")

			got := c.checkLink(tc.source, link{Destination: tc.destination})

			if tc.wantSubstr == "" {
				if got != "" {
					t.Fatalf("expected link accepted, got issue: %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Fatalf("expected issue containing %q, got %q", tc.wantSubstr, got)
			}
		})
	}
}
