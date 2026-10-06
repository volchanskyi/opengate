package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"docs/**", "docs/a/b.md", true},
		{"docs/**", "docs", false},
		{"**/*.md", "a.md", true},
		{"**/*.md", "a/b/c.md", true},
		{"**/testdata/**", "server/x/testdata/a.json", true},
		{"**/testdata/**", "testdata/golden/a.bin", true},
		{"agent/crates/*/tests/fixtures/**", "agent/crates/core/tests/fixtures/a.json", true},
		{"agent/crates/*/tests/fixtures/**", "agent/crates/core/x/tests/fixtures/a.json", false},
		{"web/src/types/api.d.ts", "web/src/types/api.d.ts", true},
		{"*.lock", "a/b.lock", false},
	}
	for _, item := range cases {
		if got := matchGlob(item.pattern, item.path); got != item.want {
			t.Errorf("matchGlob(%q, %q) = %t, want %t", item.pattern, item.path, got, item.want)
		}
	}
}

func TestClassifyKnowsEveryLanguage(t *testing.T) {
	cases := map[string]string{
		"a.go":                              langGo,
		"server/go.mod":                     langGoMod,
		"a.rs":                              langRust,
		"a.ts":                              langTS,
		"a.tsx":                             langTS,
		"a.js":                              langTS,
		"a.mjs":                             langTS,
		"a.cjs":                             langTS,
		"a.json":                            langTS,
		"a.sh":                              langShell,
		"a.bash":                            langShell,
		"a.yml":                             langYAML,
		".github/workflows/ci.yml":          langWorkflow,
		".github/actions/x/action.yml":      langWorkflow,
		"deploy/helm/c/templates/cm.yaml":   langHelm,
		"deploy/helm/c/templates/_h.tpl":    langTemplate,
		"deploy/helm/c/templates/NOTES.txt": langTemplate,
		"Makefile":                          langMake,
		"Dockerfile":                        langDocker,
		"deploy/agent.Dockerfile":           langDocker,
		"a.toml":                            langTOML,
		"a.tf":                              langHCL,
		"a.hcl":                             langHCL,
		"x/terraform.tfvars.example":        langHCL,
		"x/backend.tfbackend.example":       langHCL,
		"a.sql":                             langSQL,
		"a.rego":                            langRego,
		"a.py":                              langPython,
		"a.properties":                      langProperties,
		".gitignore":                        langHash,
		"x/.dockerignore":                   langHash,
		"x/.helmignore":                     langHash,
		".semgrepignore":                    langHash,
		".trivyignore":                      langHash,
		".shellcheckrc":                     langHash,
		".editorconfig":                     langHash,
		".github/CODEOWNERS":                langHash,
		".env.example":                      langHash,
		".claude/shell-policy.exceptions":   langHash,
		"server/x/catalogue.lock":           langHash,
		"a.css":                             langCSS,
		"a.html":                            langHTML,
	}
	for rel, want := range cases {
		got, ok := classify(rel)
		if !ok || got != want {
			t.Errorf("classify(%q) = %q, %t; want %q", rel, got, ok, want)
		}
	}
	if _, ok := classify("notes.xyz"); ok {
		t.Error("an unknown extension classifies as unknown")
	}
}

func TestScopeFileNamesReasonsAndFindsStaleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scope.tsv")
	if err := os.WriteFile(path, []byte("docs/**\tdocumentation\nvendor/**\tvendored code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadScope(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.excluded("docs/a.md") || loaded.excluded("server/a.go") {
		t.Fatal("scope excludes what it lists and nothing else")
	}
	stale := loaded.unmatched([]string{"docs/a.md", "server/a.go"})
	expectEqual(t, "stale", stale, []string{"vendor/**"})
	if err := os.WriteFile(path, []byte("docs/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScope(path); err == nil {
		t.Fatal("an entry without a reason is refused")
	}
}
