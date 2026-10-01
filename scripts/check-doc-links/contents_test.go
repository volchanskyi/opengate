package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestContentsProblems pins the contents-list rule: a page under docs/ that has
// sections starts, right after its title, with a list linking every ## to ####
// heading in order, nested by level, through the same anchors the link check
// resolves. Decision records are left out of it.
func TestContentsProblems(t *testing.T) {
	const twoSections = "# Title\n\n" +
		"- [First part](#first-part)\n" +
		"  - [A detail](#a-detail)\n" +
		"- [Second part](#second-part)\n\n" +
		"## First part\n\n### A detail\n\n## Second part\n"

	cases := []struct {
		name       string
		path       string
		content    string
		wantSubstr string // "" means the page is accepted
	}{
		{name: "a list matching every section is accepted", path: "docs/product/Page.md", content: twoSections},
		{
			name:       "a page with sections and no list is refused",
			path:       "docs/product/Page.md",
			content:    "# Title\n\n## First part\n",
			wantSubstr: "no contents list",
		},
		{
			name:       "a list that comes after an introduction is not at the top",
			path:       "docs/product/Page.md",
			content:    "# Title\n\nAn introduction.\n\n- [First part](#first-part)\n\n## First part\n",
			wantSubstr: "no contents list",
		},
		{
			name: "a section the list does not name is refused, by name",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [First part](#first-part)\n\n" +
				"## First part\n\n## Second part\n",
			wantSubstr: `"Second part"`,
		},
		{
			name: "an entry for a heading the page does not have is refused",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [First part](#first-part)\n- [Gone](#gone)\n\n" +
				"## First part\n",
			wantSubstr: "#gone",
		},
		{
			name: "entries out of order are refused",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [Second part](#second-part)\n- [First part](#first-part)\n\n" +
				"## First part\n\n## Second part\n",
			wantSubstr: "#second-part",
		},
		{
			name: "an entry nested at the wrong depth is refused",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [First part](#first-part)\n- [A detail](#a-detail)\n\n" +
				"## First part\n\n### A detail\n",
			wantSubstr: "indented",
		},
		{
			name: "an entry whose words are not the heading's is refused",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [Something else](#first-part)\n\n" +
				"## First part\n",
			wantSubstr: "Something else",
		},
		{
			name: "a repeated heading is reached through its numbered anchor",
			path: "docs/product/Page.md",
			content: "# Title\n\n" +
				"- [Setup](#setup)\n- [Later](#later)\n  - [Setup](#setup-1)\n\n" +
				"## Setup\n\n## Later\n\n### Setup\n",
		},
		{
			name: "a heading inside fenced code is not a section",
			path: "docs/product/Page.md",
			content: "# Title\n\n- [First part](#first-part)\n\n" +
				"## First part\n\n```bash\n## not a heading\n```\n",
		},
		{
			name:    "a heading below #### is not listed",
			path:    "docs/product/Page.md",
			content: "# Title\n\n- [First part](#first-part)\n\n## First part\n\n##### Fine print\n",
		},
		{name: "a page with no sections needs no list", path: "docs/product/Page.md", content: "# Title\n\nJust text.\n"},
		{
			name:       "a page with sections and no title is refused rather than let off",
			path:       "docs/product/Page.md",
			content:    "## First part\n\n## Second part\n",
			wantSubstr: "no # title",
		},
		{name: "the docs README is a page", path: "docs/README.md", content: "# Title\n\n## First part\n", wantSubstr: "no contents list"},
		{name: "a decision record is left out", path: "docs/adr/ADR-001-x.md", content: "# Title\n\n## Decision\n"},
		{name: "the decision index is left out", path: "docs/Architecture-Decision-Records.md", content: "# Title\n\n## ADR-012\n"},
		{name: "a rule outside docs is left out", path: ".claude/rules/tdd.md", content: "# Title\n\n## Section\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := contentsProblems(tc.path, []byte(tc.content))
			if tc.wantSubstr == "" {
				if len(problems) != 0 {
					t.Fatalf("expected the page accepted, got %+v", problems)
				}
				return
			}
			if len(problems) != 1 || !strings.Contains(problems[0].Message, tc.wantSubstr) {
				t.Fatalf("expected one problem containing %q, got %+v", tc.wantSubstr, problems)
			}
		})
	}
}

// TestCheckReportsContentsProblems pins that the whole-tree run, which the
// gauntlet and Docs Validate execute, reports a page whose contents list has
// fallen behind its headings.
func TestCheckReportsContentsProblems(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "docs", "product", "Page.md")
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "# Title\n\n- [First part](#first-part)\n\n## First part\n\n## Added later\n"
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	c, err := newChecker(root)
	if err != nil {
		t.Fatalf("newChecker: %v", err)
	}
	problems, err := c.check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(problems) != 1 || problems[0].Source != "docs/product/Page.md" ||
		!strings.Contains(problems[0].Message, `"Added later"`) {
		t.Fatalf("expected the stale contents list reported, got %+v", problems)
	}
}
