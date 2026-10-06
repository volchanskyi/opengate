package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSkipsNestedCheckouts(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/worktrees/agent/.git", "gitdir: elsewhere\n")
	write(".claude/worktrees/agent/docs/broken.md", "# Broken\n\n[gone](missing.md)\n")
	write(".claude/worktrees/agent/.claude/rules/broken.md", "# Broken\n\n[gone](missing.md)\n")
	write("docs/durable.md", "# Durable\n\n[gone](missing-too.md)\n")

	c, err := newChecker(root)
	if err != nil {
		t.Fatal(err)
	}
	problems, err := c.check()
	if err != nil {
		t.Fatal(err)
	}
	reported := false
	for _, item := range problems {
		if strings.HasPrefix(item.Source, ".claude/worktrees/") {
			t.Errorf("a nested checkout was scanned: %s:%d %s", item.Source, item.Line, item.Message)
		}
		if item.Source == "docs/durable.md" {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("the repository's own broken link is reported, got %+v", problems)
	}
}
