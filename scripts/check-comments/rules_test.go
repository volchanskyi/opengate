package main

import (
	"strings"
	"testing"
)

func goFile(lines ...string) string {
	return "package p\n\n" + strings.Join(lines, "\n") + "\nvar x = 1\n"
}

func TestLengthAllowsTwoLinesAndRefusesThree(t *testing.T) {
	expectEqual(t, "two", violationsOf(t, "p.go", goFile("// One.", "// Two.")), nil)
	expectEqual(t, "three", violationsOf(t, "p.go", goFile("// One.", "// Two.", "// Three.")), []string{"3:length"})
	expectEqual(t, "blank between", violationsOf(t, "p.go", goFile("// One.", "//", "// Two.")), []string{"3:length"})
}

func TestLengthSkipsDelimiterLinesAndDirectives(t *testing.T) {
	expectEqual(t, "jsdoc", violationsOf(t, "a.ts", "/**\n * One.\n * Two.\n */\nexport const x = 1;\n"), nil)
	expectEqual(t, "directive", violationsOf(t, "p.go", goFile("// One.", "// Two.", "//", "//go:generate echo hi")), nil)
	expectEqual(t, "trailing lines stand alone", violationsOf(t, "p.go", "package p\n\nvar a = 1 // One.\nvar b = 2 // Two.\nvar c = 3 // Three.\n"), nil)
}

func TestLengthAllowsInterfaceLists(t *testing.T) {
	script := strings.Join([]string{
		"#!/usr/bin/env bash",
		"# Prints a greeting.",
		"# Usage: run.sh NAME",
		"#",
		"# Environment:",
		"#   NAME_PREFIX  (required) the prefix.",
		"#   NAME_SUFFIX  the suffix.",
		"#",
		"# Exit codes: 0 = printed;",
		"#             1 = refused;",
		"#             2 = missing input.",
		"echo hi",
		"",
	}, "\n")
	expectEqual(t, "lists", violationsOf(t, "run.sh", script), nil)
	properties := "# JUSTIFICATIONS (one line per entry):\n#   **/a/** — test code\n#   **/b/** — test code\n#   **/c/** — test code\nk=v\n"
	expectEqual(t, "justifications", violationsOf(t, "sonar-project.properties", properties), nil)
}

func TestLengthCountsListContinuationsAsProse(t *testing.T) {
	script := "#!/usr/bin/env bash\n# Environment:\n#   A  the first.\n#      continues.\n#      again.\n#      more.\necho hi\n"
	expectEqual(t, "continuations", violationsOf(t, "run.sh", script), []string{"2:length"})
	prose := "#!/usr/bin/env bash\n# One.\n# Two.\n# Usage: run.sh\n# Three.\n# Four.\necho hi\n"
	expectEqual(t, "prose around a list", violationsOf(t, "run.sh", prose), []string{"2:length"})
}

func TestWidthMeasuresTheCommentNotTheIndent(t *testing.T) {
	wide := "// " + strings.Repeat("w", 98)
	expectEqual(t, "at the limit", violationsOf(t, "p.go", goFile("\t\t\t\t"+wide[:100])), nil)
	expectEqual(t, "over the limit", violationsOf(t, "p.go", goFile(wide)), []string{"3:width"})
	expectEqual(t, "directive", violationsOf(t, "p.go", goFile("//go:generate "+strings.Repeat("x", 120))), nil)
}

func TestDocReferences(t *testing.T) {
	refused := []string{
		"// Follows ADR-123.",
		"// The ADR says so.",
		"// Follows docs/plan.md here.",
		"// Lands in WS-19.",
		"// Lands in WS0.",
		"// Sits in Phase B.",
		"// Sits in Phase 13b.",
		"// Per § 4.",
	}
	for _, line := range refused {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), []string{"3:doc-ref"})
	}
	allowed := []string{
		"// Bounds it per RFC 9000 §8.1.",
		"// Bounds it per RFC 7616 § 3.4.",
		"// Follows .claude/rules/code-comments.md for its shape.",
		"// Follows rules/tdd.md for its order.",
		"// Runs one phase before the commit.",
		"// Names CVE-2024-1234 and GHSA-abcd-efgh-ijkl and RUSTSEC-2024-0001.",
	}
	for _, line := range allowed {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), nil)
	}
	named := "package p\n\nconst guide = \"docs/guide.md\"\n\n// Reads docs/guide.md for the table.\nvar x = 1\n"
	expectEqual(t, "path the code names", violationsOf(t, "p.go", named), nil)
}

func TestLinks(t *testing.T) {
	refused := []string{
		"// Reads https://github.com/org/repo for data.",
		"// Landed in PR 12.",
		"// Landed in PR #12.",
		"// Tracks #123 upstream.",
		"// Failed in run 26929821908.",
		"// Failed in CI run 26929821908.",
		"// Arrived in commit 2acbdbdc.",
		"// Arrived at 2acbdbdc: two fixes.",
	}
	for _, line := range refused {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), []string{"3:link"})
	}
	allowed := []string{
		"// Answers on https://example.com/path and api.example.org.",
		"// Answers on http://localhost:8080/health.",
		"// Resolves agent.invalid and host.test locally.",
		"// Holds 0xdeadbeef and the word facade.",
		"// Holds 1234567 retries.",
	}
	for _, line := range allowed {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), nil)
	}
	named := "package p\n\nconst u = \"https://get.helm.sh/helm-v3.tar.gz\"\n\n// Downloads https://get.helm.sh/helm-v3.tar.gz once.\nvar x = 1\n"
	expectEqual(t, "url the code names", violationsOf(t, "p.go", named), nil)
}

func TestDates(t *testing.T) {
	expectEqual(t, "date", violationsOf(t, "p.go", goFile("// Reads 2026-09-13.")), []string{"3:date"})
	expectEqual(t, "timestamp", violationsOf(t, "p.go", goFile("// Fires at 2026-09-13T04:00:00Z.")), nil)
	expectEqual(t, "spaced timestamp", violationsOf(t, "p.go", goFile("// Fires at 2026-09-13 04:00.")), nil)
}

func TestHistoryNegationPersonDivider(t *testing.T) {
	cases := map[string]string{
		"// Previously retried.":             "history",
		"// Formerly a cache.":               "history",
		"// Historically slow.":              "history",
		"// No longer retries.":              "history",
		"// Holds until now.":                "history",
		"// Last checked by hand.":           "history",
		"// Kept for rollback.":              "history",
		"// Dormant path.":                   "history",
		"// It is not a cache.":              "negation",
		"// They are not the owners.":        "negation",
		"// It isn't cached.":                "negation",
		"// They aren't cached.":             "negation",
		"// Holds a copy rather than a ref.": "negation",
		"// Holds a copy instead of a ref.":  "negation",
		"// Unlike the store, it waits.":     "negation",
		"// Not only waits, it retries.":     "negation",
		"// We retry.":                       "person",
		"// Retries what we're given.":       "person",
		"// Holds what we've seen.":          "person",
		"// Says we'd retry.":                "person",
		"// Holds our state.":                "person",
		"// The state is ours.":              "person",
		"// ----":                            "divider",
		"// ====":                            "divider",
		"// ****":                            "divider",
		"// ────":                            "divider",
		"// ####":                            "divider",
	}
	for line, code := range cases {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), []string{"3:" + code})
	}
	allowed := []string{
		"// Retries a weak write.",
		"// Holds the overall count.",
		"// Waits --- then retries.",
		"// Is not retried: the caller decides.",
	}
	for _, line := range allowed {
		expectEqual(t, line, violationsOf(t, "p.go", goFile(line)), nil)
	}
}

func TestPhrasesSpanningTwoLines(t *testing.T) {
	expectEqual(t, "split phrase", violationsOf(t, "p.go", goFile("// Holds a copy rather", "// than a reference.")), []string{"3:negation"})
}

func TestEveryLanguageAppliesTheRules(t *testing.T) {
	cases := map[string]string{
		"a.sh":                          "#!/usr/bin/env bash\n# We print.\necho hi\n",
		"a.rs":                          "// We print.\nfn main() {}\n",
		"a.ts":                          "// We print.\nexport {};\n",
		"a.yml":                         "# We print.\na: 1\n",
		".github/workflows/w.yml":       "jobs:\n  j:\n    steps:\n      - run: |\n          # We print.\n          echo hi\n",
		"Makefile":                      "# We print.\na:\n",
		"Dockerfile":                    "# We print.\nFROM a\n",
		"a.toml":                        "# We print.\na = 1\n",
		"a.tf":                          "# We print.\n",
		"a.sql":                         "-- We print.\n",
		"a.rego":                        "# We print.\n",
		"a.py":                          "# We print.\n",
		"a.properties":                  "# We print.\n",
		".dockerignore":                 "# We print.\n",
		"a.css":                         "/* We print. */\n",
		"a.html":                        "<!-- We print. -->\n",
		"deploy/helm/c/templates/a.tpl": "{{/* We print. */}}\n",
	}
	for rel, content := range cases {
		found := violationsOf(t, rel, content)
		if len(found) != 1 || !strings.HasSuffix(found[0], ":person") {
			t.Errorf("%s: want one person violation, got %q", rel, found)
		}
	}
}

func TestDirectivesSurviveEveryRule(t *testing.T) {
	expectEqual(t, "go", violationsOf(t, "p.go", "// Code generated by tool from ADR-12 spec. DO NOT EDIT.\n\npackage p\n"), nil)
	expectEqual(t, "nosec", violationsOf(t, "p.go", goFile("\tx := uint32(1) // #nosec G115 -- bounded above by the frame size, which keeps every length within thirty-two bits.")), nil)
	expectEqual(t, "shellcheck", violationsOf(t, "a.sh", "#!/usr/bin/env bash\n# shellcheck disable=SC2034\nx=1\n"), nil)
	expectEqual(t, "ts", violationsOf(t, "a.ts", "/// <reference types=\"vite/client\" />\n// @ts-nocheck\nexport {};\n"), nil)
	expectEqual(t, "hadolint", violationsOf(t, "Dockerfile", "# hadolint ignore=DL3018\nFROM a\n"), nil)
	expectEqual(t, "jsdoc type", violationsOf(t, "a.cjs", "/** @type {import('x').Config} */\nmodule.exports = {};\n"), nil)
}
