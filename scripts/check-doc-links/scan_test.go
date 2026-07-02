package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckExcludesPlansFromScanScope pins the root-cause fix: files under
// .claude/plans/ (active plans and archive/) are ephemeral and deletion-bound,
// so their internal links rot by design. The checker must not scan them as link
// SOURCES — a broken link inside a plan is ignored, while an identical broken
// link in a durable doc under docs/ is still reported. (Links TO plan files from
// durable sources remain governed by the plan-link policy; see checker_test.go.)
func TestCheckExcludesPlansFromScanScope(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Broken links inside plan files (active and archived) resolving to a
	// missing, non-plan target — a plain "target does not exist" that today is
	// reported and must stop being reported once plans leave the scan scope.
	mustWrite(".claude/plans/active.md", "# Active plan\n\n[gone](../nonexistent-target.md)\n")
	mustWrite(".claude/plans/archive/old.md", "# Archived plan\n\n[gone](../../nonexistent-target.md)\n")
	// A broken link in a durable doc must still be reported.
	mustWrite("docs/durable.md", "# Durable\n\n[gone](nope-docs.md)\n")

	c, err := newChecker(root)
	if err != nil {
		t.Fatalf("newChecker: %v", err)
	}
	problems, err := c.check()
	if err != nil {
		t.Fatalf("check: %v", err)
	}

	var docsReported bool
	for _, p := range problems {
		if strings.HasPrefix(p.Source, ".claude/plans/") {
			t.Errorf("plan file scanned as link source: %s:%d %s", p.Source, p.Line, p.Message)
		}
		if p.Source == "docs/durable.md" {
			docsReported = true
		}
	}
	if !docsReported {
		t.Fatalf("expected broken link in docs/durable.md to be reported; got %+v", problems)
	}
}
