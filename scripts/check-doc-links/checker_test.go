package main

import (
	"strings"
	"testing"
)

// TestCheckLinkPlanPolicy pins the plan-link doctrine: a plan is a working
// document and is deleted in the commit that lands its work, so nothing
// durable may depend on one. No source under docs/ — ADRs included — and no
// source under .claude/ may link a plan. A plan linking a sibling plan is the
// working area referring to itself and is left alone.
// See .claude/rules/plans-and-adrs.md.
func TestCheckLinkPlanPolicy(t *testing.T) {
	const planTarget = ".claude/plans/foo.md"

	cases := []struct {
		name        string
		source      string
		destination string
		wantSubstr  string // "" means the link must be accepted
	}{
		{
			name:        "doc to plan is refused",
			source:      "docs/infrastructure/Testing.md",
			destination: "../../.claude/plans/foo.md",
			wantSubstr:  "must not link plan files",
		},
		{
			name:        "ADR to plan is refused",
			source:      "docs/adr/ADR-037-example.md",
			destination: "../../.claude/plans/foo.md",
			wantSubstr:  "must not link plan files",
		},
		{
			name:        "rule to plan is refused",
			source:      ".claude/rules/tdd.md",
			destination: "../plans/foo.md",
			wantSubstr:  "must not link plan files",
		},
		{
			name:        "repository README to plan is refused",
			source:      "README.md",
			destination: ".claude/plans/foo.md",
			wantSubstr:  "must not link plan files",
		},
		{
			name:        "plan to sibling plan is allowed",
			source:      ".claude/plans/bar.md",
			destination: "foo.md",
			wantSubstr:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := newChecker(t.TempDir())
			if err != nil {
				t.Fatalf("newChecker: %v", err)
			}
			// Make the target resolvable so an "allowed" case reaches a clean
			// result instead of a missing-target error.
			c.overlays[planTarget] = []byte("# plan\n")

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
