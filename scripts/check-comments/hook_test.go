package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyEditsHonoursOrderAndReplaceAll(t *testing.T) {
	got, err := applyEdits([]byte("a a b"), []edit{
		{OldString: "a", NewString: "c", ReplaceAll: true},
		{OldString: "c c", NewString: "d"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "d b" {
		t.Fatalf("got %q", got)
	}
	if _, err := applyEdits([]byte("a"), []edit{{OldString: "z", NewString: "y"}}); err == nil {
		t.Fatal("an edit whose old_string is absent is refused")
	}
}

func TestRepositoryRootIsFoundFromTheFile(t *testing.T) {
	dir := t.TempDir()
	worktree := filepath.Join(dir, "main", ".claude", "worktrees", "w")
	if err := os.MkdirAll(filepath.Join(worktree, "server"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "main", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, ok := repositoryRoot(filepath.Join(worktree, "server", "new.go"))
	if !ok || root != worktree {
		t.Fatalf("root = %q, %t; want %q", root, ok, worktree)
	}
	if _, ok := repositoryRoot(filepath.Join(dir, "elsewhere", "x.go")); ok {
		t.Fatal("a path under no repository has no root")
	}
}

func TestHookReportsOnlyNewViolations(t *testing.T) {
	before := extractOne(t, "p.go", "package p\n\n// We keep this.\nvar a = 1\n")
	after := extractOne(t, "p.go", "package p\n\n// We keep this.\nvar a = 2\n\n// Our new one.\nvar b = 1\n")
	added := onlyNew(analyze(after), analyze(before))
	if len(added) != 1 || added[0].Line != 6 || added[0].Code != "person" {
		t.Fatalf("got %v", added)
	}
	if !strings.Contains(added[0].String(), "p.go:6: person: ") {
		t.Fatalf("violation prints as path:line: code: text, got %q", added[0].String())
	}
}
