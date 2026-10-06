package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxWidth = 100

var directivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^#!`),
	regexp.MustCompile(`^//go:\S`),
	regexp.MustCompile(`^//(line|export|extern) `),
	regexp.MustCompile(`^// \+build `),
	regexp.MustCompile(`^//\s*#nosec\b`),
	regexp.MustCompile(`^//\s*nolint\b`),
	regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`),
	regexp.MustCompile(`^//\s*@ts-(expect-error|nocheck|ignore|check)\b`),
	regexp.MustCompile(`^/// <reference `),
	regexp.MustCompile(`^//\s*@vitest-environment\b`),
	regexp.MustCompile(`^/\*\s*@vite-ignore\s*\*/$`),
	regexp.MustCompile(`^/\*\*\s*@type\s`),
	regexp.MustCompile(`^(//|/\*)\s*eslint-`),
	regexp.MustCompile(`^#\s*shellcheck\s`),
	regexp.MustCompile(`^#\s*hadolint\s`),
	regexp.MustCompile(`^#\s*(syntax|escape|check)=`),
	regexp.MustCompile(`^#\s*v[0-9]+(\.[0-9]+)*$`),
	regexp.MustCompile(`^#\s*yaml-language-server:`),
	regexp.MustCompile(`^#\s*(-\*-|type:|noqa|pragma:)`),
	regexp.MustCompile(`^#\s*(tflint-ignore|checkov:skip|trivy:ignore|tfsec:ignore)`),
}

func isDirective(line int, text string) bool {
	first, _, _ := strings.Cut(text, "\n")
	first = strings.TrimSpace(first)
	if strings.HasPrefix(first, "#!") && line != 1 {
		return false
	}
	for _, pattern := range directivePatterns {
		if pattern.MatchString(first) {
			return true
		}
	}
	return false
}

type phraseRule struct {
	code    string
	pattern *regexp.Regexp
	allow   func(match, before, after, code string) bool
}

var (
	exampleHost  = regexp.MustCompile(`(?i)^(localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\])$|(^|\.)(example\.(com|org|net)|example|invalid|test|localhost)$`)
	standardRef  = regexp.MustCompile(`\b(RFC|ISO|IEC|IEEE|NIST SP|FIPS|ECMA-?|ITU-T [A-Z]\.?)\s?[0-9][0-9.:-]*,?\s*$`)
	ruleFile     = regexp.MustCompile(`(^|/)rules/[a-z0-9-]+\.md$`)
	hexHash      = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	urlHost      = regexp.MustCompile(`^[a-z]+://([^/:?#]+)`)
	urlTrailing  = ".,;:)]'\""
	phraseRules  = []phraseRule{}
	dateWithTime = regexp.MustCompile(`^(T[0-9]|[ ][0-9]{2}:[0-9]{2})`)
)

func init() {
	phraseRules = []phraseRule{
		{"doc-ref", regexp.MustCompile(`\bADRs?\b`), nil},
		{"doc-ref", regexp.MustCompile(`\bWS-?[0-9]+[a-z]?\b|\bWS-[A-Z]\b`), nil},
		{"doc-ref", regexp.MustCompile(`\bPhase [A-Z0-9][A-Za-z0-9]*\b`), nil},
		{"doc-ref", regexp.MustCompile(`[A-Za-z0-9_./-]*[A-Za-z0-9_]\.md\b`), allowMarkdown},
		{"doc-ref", regexp.MustCompile(`§`), allowSection},
		{"link", regexp.MustCompile(`\b(https?|wss?|ftp)://[^\s<>"'` + "`" + `]+`), allowURL},
		{"link", regexp.MustCompile(`\bPRs? ?#?[0-9]+\b|\bPR-[A-Z0-9]+\b|\b(?i:pull request) #?[0-9]+`), nil},
		{"link", regexp.MustCompile(`(^|[\s(\[])#[0-9]+\b`), nil},
		{"link", regexp.MustCompile(`(?i)\bruns? (id )?#?[0-9]{6,}\b`), nil},
		{"link", regexp.MustCompile(`(?i)\bcommits? [0-9a-f]{7,40}\b`), nil},
		{"link", hexHash, allowHex},
		{"date", regexp.MustCompile(`\b(19|20)[0-9]{2}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])\b`), allowTimestamp},
		{"history", regexp.MustCompile(`(?i)\b(previously|formerly|historically|no longer|until now|last checked|kept for rollback|dormant)\b`), nil},
		{"negation", regexp.MustCompile(`(?i)\b(is|are) not (a|an|the)\b|\b(isn|aren)['’]t\b|\brather than\b|\binstead of\b|\bunlike\b|\bnot only\b`), nil},
		{"person", regexp.MustCompile(`(?i)\b(we|we['’]re|we['’]ve|we['’]d|our|ours)\b`), nil},
		{"divider", regexp.MustCompile(`[-=#*─]{4,}`), nil},
	}
}

func allowMarkdown(match, _, _, code string) bool {
	return ruleFile.MatchString(match) || strings.Contains(code, match)
}

func allowSection(_, before, _, _ string) bool {
	return standardRef.MatchString(before)
}

func allowURL(match, _, _, code string) bool {
	trimmed := strings.TrimRight(match, urlTrailing)
	if host := urlHost.FindStringSubmatch(strings.ToLower(trimmed)); host != nil && exampleHost.MatchString(host[1]) {
		return true
	}
	return strings.Contains(code, trimmed)
}

func allowHex(match, _, _, code string) bool {
	if !strings.ContainsAny(match, "0123456789") || !strings.ContainsAny(match, "abcdef") {
		return true
	}
	return strings.Contains(code, match)
}

func allowTimestamp(_, _, after, _ string) bool {
	return dateWithTime.MatchString(after)
}

func analyze(result fileResult) []violation {
	if result.binary || result.err != nil {
		return nil
	}
	lines := commentLines(result)
	if len(lines) == 0 {
		return nil
	}
	code := codeText(result)
	found := map[string]violation{}
	add := func(line int, rule, text string) {
		key := rule + "\t" + strconv.Itoa(line)
		if _, ok := found[key]; !ok {
			found[key] = violation{Path: result.rel, Line: line, Code: rule, Text: text}
		}
	}
	for _, block := range groupBlocks(lines) {
		if blockLength(block) > 2 {
			first, _ := firstProse(block)
			add(first.Line, "length", joinBodies(block))
		}
		checkPhrases(block, code, add)
	}
	for _, line := range lines {
		if !line.Directive && runeWidth(line.Raw) > maxWidth {
			add(line.Line, "width", line.Body)
		}
	}
	for _, line := range testDocLines(result, lines) {
		add(line.Line, "test-doc", line.Body)
	}
	out := make([]violation, 0, len(found))
	for _, item := range found {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func checkPhrases(block []commentLine, code string, add func(int, string, string)) {
	var joined strings.Builder
	var starts []int
	var kept []commentLine
	for _, line := range block {
		if line.Directive {
			continue
		}
		if joined.Len() > 0 {
			joined.WriteString(" ")
		}
		starts = append(starts, joined.Len())
		kept = append(kept, line)
		joined.WriteString(line.Body)
	}
	text := joined.String()
	for _, rule := range phraseRules {
		for _, loc := range rule.pattern.FindAllStringIndex(text, -1) {
			match := strings.TrimLeft(text[loc[0]:loc[1]], " \t([")
			if rule.allow != nil && rule.allow(match, text[:loc[0]], text[loc[1]:], code) {
				continue
			}
			at := sort.Search(len(starts), func(i int) bool { return starts[i] > loc[0] }) - 1
			if at < 0 {
				at = 0
			}
			add(kept[at].Line, rule.code, kept[at].Body)
		}
	}
}

func joinBodies(block []commentLine) string {
	var parts []string
	for _, line := range block {
		if strings.TrimSpace(line.Body) != "" {
			parts = append(parts, strings.TrimSpace(line.Body))
		}
	}
	return strings.Join(parts, " ")
}
