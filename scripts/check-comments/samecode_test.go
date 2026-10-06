package main

import "testing"

func sameCode(t *testing.T, rel, before, after string) bool {
	t.Helper()
	return codeEqual(extractOne(t, rel, before), extractOne(t, rel, after))
}

func TestSameCodeIgnoresCommentsAndWhitespace(t *testing.T) {
	cases := []struct {
		rel, before, after string
	}{
		{"p.go", "package p\n\n// A long story.\n// More.\nvar a = 1 // tail\n", "package p\n\nvar a = 1\n"},
		{"lib.rs", "/// Doc.\nfn a() { let s = \"x\"; } // tail\n", "fn a() { let s = \"x\"; }\n"},
		{"a.ts", "/** Doc. */\nexport const a = 1; // tail\n", "export const a = 1;\n"},
		{"c.tsx", "export const C = () => (\n  <p>\n    {/* Note. */}\n    text\n  </p>\n);\n", "export const C = () => (\n  <p>\n    text\n  </p>\n);\n"},
		{"s.sh", "#!/usr/bin/env bash\n# Note.\necho hi # tail\n", "#!/usr/bin/env bash\necho hi\n"},
		{"c.yml", "# Note.\na: 1 # tail\n\n\nb: 2\n", "a: 1\n\nb: 2\n"},
		{".github/workflows/w.yml", "jobs:\n  j:\n    steps:\n      - run: |\n          # Note.\n          echo hi\n", "jobs:\n  j:\n    steps:\n      - run: |\n          echo hi\n"},
	}
	for _, item := range cases {
		if !sameCode(t, item.rel, item.before, item.after) {
			t.Errorf("%s: comment-only edit reads as a code change", item.rel)
		}
	}
}

func TestSameCodeSeesCodeChanges(t *testing.T) {
	cases := []struct {
		rel, before, after string
	}{
		{"p.go", "package p\n\nvar a = 1\n", "package p\n\nvar a = 2\n"},
		{"p.go", "package p\n\nvar s = \"a  b\"\n", "package p\n\nvar s = \"a b\"\n"},
		{"lib.rs", "fn a() { 1 }\n", "fn a() { 2 }\n"},
		{"a.ts", "export const a = 1;\n", "export const a = 2;\n"},
		{"c.tsx", "export const C = () => <p>one</p>;\n", "export const C = () => <p>two</p>;\n"},
		{"s.sh", "#!/usr/bin/env bash\necho hi\n", "#!/usr/bin/env bash\necho bye\n"},
		{"s.sh", "#!/usr/bin/env bash\ncat <<'EOF'\na\n\nb\nEOF\n", "#!/usr/bin/env bash\ncat <<'EOF'\na\nb\nEOF\n"},
		{"c.yml", "a: 1\n", "a: 2\n"},
		{"c.yml", "s: |\n  a\n\n  b\n", "s: |\n  a\n  b\n"},
		{"c.yml", "a:\n  b: 1\n", "a:\nb: 1\n"},
	}
	for _, item := range cases {
		if sameCode(t, item.rel, item.before, item.after) {
			t.Errorf("%s: a code change reads as identical (%q -> %q)", item.rel, item.before, item.after)
		}
	}
}

func TestNewCommentsAreFoundByPosition(t *testing.T) {
	base := extractOne(t, "p.go", "package p\n\n// A holds one.\nvar a = 1\n\nvar b = 2 // B holds two.\n\nvar c = 3\n")
	moved := extractOne(t, "p.go", "package p\n\n// A holds exactly one.\nvar a = 1\n\n// B holds two.\nvar b = 2\n\nvar c = 3\n")
	if added := newComments(base, moved); len(added) != 0 {
		t.Fatalf("a rewritten comment and a trailing comment moved above its line are not new, got %v", added)
	}
	added := newComments(base, extractOne(t, "p.go", "package p\n\n// A holds one.\nvar a = 1\n\nvar b = 2\n\n// C holds three.\nvar c = 3\n"))
	if len(added) != 1 || added[0] != 8 {
		t.Fatalf("a comment above c is new at line 8, got %v", added)
	}
}
