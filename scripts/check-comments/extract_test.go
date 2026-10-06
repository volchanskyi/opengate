package main

import (
	"strings"
	"testing"
)

func TestGoExtractorSkipsStringsAndRawStrings(t *testing.T) {
	content := "package p\n\n// Doc states one fact.\nconst s = \"// not a comment\"\n\nvar r = `/* not a comment */`\n\nvar x = 1 // trailing fact\n\n/*\nBlock fact.\n*/\nvar y = 2\n"
	expectEqual(t, "go comments", bodiesOf(t, "p.go", content), []string{
		"3:true:Doc states one fact.",
		"8:false:trailing fact",
		"10:true:",
		"11:true:Block fact.",
		"12:true:",
	})
}

func TestGoExtractorToleratesSyntaxErrors(t *testing.T) {
	content := "package p\n\nfunc (\n// Survives the error.\n"
	expectEqual(t, "go comments", bodiesOf(t, "p.go", content), []string{"4:true:Survives the error."})
}

func TestRustLexerSeparatesCommentsFromLiterals(t *testing.T) {
	content := strings.Join([]string{
		"//! Crate fact.",
		"/// Item fact.",
		"pub struct H<'a> { v: &'a str }",
		"fn q() -> char { '\"' }",
		"fn e() -> char { '\\'' }",
		"fn s() -> (&'static str, &'static str, &'static [u8], u8) { (r#\"// no\"#, \"/* no */\", b\"// no\", b'\"') }",
		"fn c() { let _ = c\"// no\"; let _ = cr#\"/* no\"#; let r#type = 1; }",
		"/* outer /* inner */ still",
		"   closes here */",
		"fn l() { 'a: loop { break 'a; } } // tail",
		"",
	}, "\n")
	expectEqual(t, "rust comments", bodiesOf(t, "lib.rs", content), []string{
		"1:true:Crate fact.",
		"2:true:Item fact.",
		"8:true:outer /* inner */ still",
		"9:true:closes here",
		"10:false:tail",
	})
}

func TestTypeScriptExtractorUsesTheParser(t *testing.T) {
	content := strings.Join([]string{
		"/**",
		" * Pattern fact.",
		" */",
		"export const p = /\\/\\/ no/;",
		"export const t = `// no ${p.source} /* no */`;",
		"export const d = 4 / 2; // tail",
		"",
	}, "\n")
	expectEqual(t, "ts comments", bodiesOf(t, "a.ts", content), []string{
		"1:true:",
		"2:true:Pattern fact.",
		"3:true:",
		"6:false:tail",
	})
}

func TestTypeScriptExtractorSkipsJSXText(t *testing.T) {
	content := "export const C = () => (\n  <p>\n    // no\n    {/* JSX fact. */}\n  </p>\n);\n"
	expectEqual(t, "tsx comments", bodiesOf(t, "c.tsx", content), []string{"4:true:JSX fact."})
}

func TestTypeScriptExtractorReadsJSONWithComments(t *testing.T) {
	content := "{\n  // Key fact.\n  \"k\": \"// no\"\n}\n"
	expectEqual(t, "json comments", bodiesOf(t, "tsconfig.json", content), []string{"2:true:Key fact."})
}

func TestShellExtractorUsesTheSyntaxTree(t *testing.T) {
	content := "#!/usr/bin/env bash\n# Script fact.\nv=\"# no\"\necho \"${#v}\" # tail\ncat <<'EOF'\n# no\nEOF\n"
	expectEqual(t, "shell comments", bodiesOf(t, "s.sh", content), []string{
		"1:true:!/usr/bin/env bash",
		"2:true:Script fact.",
		"4:false:tail",
	})
}

func TestShellExtractorReadsABareHashAsAComment(t *testing.T) {
	content := "#!/usr/bin/env bash\n# First fact.\n#\n# Second fact.\nset -e\n"
	expectEqual(t, "shell comments", bodiesOf(t, "s.sh", content), []string{
		"1:true:!/usr/bin/env bash",
		"2:true:First fact.",
		"3:true:",
		"4:true:Second fact.",
	})
	expectEqual(t, "shell violations", violationsOf(t, "s.sh", content), []string{"2:length"})
	workflow := "jobs:\n  j:\n    steps:\n      - run: |\n          # First fact.\n          #\n          # Second fact.\n          true\n"
	expectEqual(t, "workflow violations", violationsOf(t, ".github/workflows/w.yml", workflow), []string{"5:length"})
}

func TestShellExtractorRefusesWhatShfmtRejects(t *testing.T) {
	results, err := testEnv(t).extractAll([]source{{rel: "b.sh", content: []byte("if then fi (\n")}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].err == nil || !strings.Contains(results[0].err.Error(), "parse") {
		t.Fatalf("want a parse error, got %v", results[0].err)
	}
}

func TestYAMLExtractorHonoursQuotesAndBlockScalars(t *testing.T) {
	content := strings.Join([]string{
		"# File fact.",
		"a: \"x # no\"",
		"b: 'it''s # no'",
		"c: it's # plain fact",
		"d: |",
		"  # no",
		"  text",
		"e: https://example.com/#no",
		"f:",
		"  - g # item fact",
		"",
	}, "\n")
	expectEqual(t, "yaml comments", bodiesOf(t, "c.yml", content), []string{
		"1:true:File fact.",
		"4:false:plain fact",
		"10:false:item fact",
	})
}

func TestWorkflowRunBlocksAreShell(t *testing.T) {
	content := strings.Join([]string{
		"jobs:",
		"  j:",
		"    steps:",
		"      - uses: a/b@0123456789abcdef0123456789abcdef01234567 # v4",
		"      - run: |",
		"          # Step fact.",
		"          echo \"# no\"",
		"          cat <<'EOF'",
		"          # no",
		"          EOF",
		"        shell: bash # shell fact",
		"      - with:",
		"          script: |",
		"            // no",
		"",
	}, "\n")
	expectEqual(t, "workflow comments", bodiesOf(t, ".github/workflows/w.yml", content), []string{
		"4:false:v4",
		"6:true:Step fact.",
		"11:false:shell fact",
	})
}

func TestHelmTemplatesReadTemplateComments(t *testing.T) {
	content := "{{- /* Chart fact. */ -}}\nkind: ConfigMap # kind fact\nname: {{ printf \"# no\" }}\ndata:\n  s: |\n    # no\n{{/* Two\n   lines. */}}\n"
	expectEqual(t, "helm comments", bodiesOf(t, "deploy/helm/c/templates/cm.yaml", content), []string{
		"1:true:Chart fact.",
		"2:false:kind fact",
		"7:true:Two",
		"8:true:lines.",
	})
}

func TestLineLanguagesHonourTheirStrings(t *testing.T) {
	cases := []struct {
		rel     string
		content string
		want    []string
	}{
		{"Makefile", "# Make fact.\nb:\n\t@echo \"# no\"\n\t@echo 'no # no' # recipe fact\nV = 1 # var fact\n", []string{"1:true:Make fact.", "4:false:recipe fact", "5:false:var fact"}},
		{"Dockerfile", "# Docker fact.\nFROM a\nRUN echo \"# no\" # no\n", []string{"1:true:Docker fact."}},
		{"deploy/agent.Dockerfile", "# syntax=docker/dockerfile:1\nFROM a\n", []string{"1:true:syntax=docker/dockerfile:1"}},
		{"x.toml", "# Toml fact.\na = \"# no\"\nb = '# no'\nc = \"\"\"\n# no\n\"\"\"\nd = 1 # tail\n", []string{"1:true:Toml fact.", "7:false:tail"}},
		{"m.tf", "# Hash fact.\n// Slash fact.\na = \"# no\" # tail\nb = <<-EOT\n  # no\nEOT\n/* Block\n   fact. */\n", []string{"1:true:Hash fact.", "2:true:Slash fact.", "3:false:tail", "7:true:Block", "8:true:fact."}},
		{"s.sql", "-- Sql fact.\nSELECT '-- no';\nCREATE FUNCTION f() AS $$ -- no\n$$;\nCREATE FUNCTION g() AS $body$\n-- no\n$body$;\n/* Block fact. */\n", []string{"1:true:Sql fact.", "8:true:Block fact."}},
		{"p.rego", "# Rego fact.\na := \"# no\"\nb := `# no`\n", []string{"1:true:Rego fact."}},
		{"t.py", "#!/usr/bin/env python3\n\"\"\"# no\n# no\"\"\"\n# Py fact.\nx = '# no'  # tail\n", []string{"1:true:!/usr/bin/env python3", "4:true:Py fact.", "5:false:tail"}},
		{"a.properties", "# Prop fact.\n! Bang fact.\nk=v # no\nl=a,\\\n  # no\n", []string{"1:true:Prop fact.", "2:true:Bang fact."}},
		{".gitignore", "# Ignore fact.\n/out/\n", []string{"1:true:Ignore fact."}},
		{".github/CODEOWNERS", "# Owner fact.\n* @someone\n", []string{"1:true:Owner fact."}},
		{".editorconfig", "# Editor fact.\n; Semicolon fact.\n[*]\n", []string{"1:true:Editor fact.", "2:true:Semicolon fact."}},
		{"server/go.mod", "// Mod fact.\nmodule m\n", []string{"1:true:Mod fact."}},
		{"deploy/helm/c/templates/_helpers.tpl", "{{/* Tpl fact. */}}\n{{- define \"x\" -}}{{ printf \"/* no */\" }}{{- end -}}\n", []string{"1:true:Tpl fact."}},
		{"s.css", "/* Css fact. */\n.a::before { content: \"/* no */\"; }\n", []string{"1:true:Css fact."}},
		{"i.html", "<!-- Html fact. -->\n<p>&lt;!-- no --&gt;</p><script>const a = \"<!-- no -->\";</script>\n", []string{"1:true:Html fact."}},
	}
	for _, item := range cases {
		expectEqual(t, item.rel, bodiesOf(t, item.rel, item.content), item.want)
	}
}

func TestExtractorsMarkBinaryContentOutOfScope(t *testing.T) {
	result := extractOne(t, "a.go", "package p\x00\n// no\n")
	if len(commentLines(result)) != 0 {
		t.Fatalf("binary content yields no comments, got %v", commentLines(result))
	}
}

func TestExtractorsHandleCRLF(t *testing.T) {
	expectEqual(t, "crlf", bodiesOf(t, "p.go", "package p\r\n\r\n// Fact.\r\nvar x = 1\r\n"), []string{"3:true:Fact."})
}
