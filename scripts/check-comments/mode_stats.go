package main

import (
	"fmt"
	"sort"
	"strings"
)

func stats(opts options, paths []string) (int, error) {
	_, _, results, err := extractScoped(opts, paths, false)
	if err != nil {
		return 0, err
	}
	byLang, byArea := map[string]int{}, map[string]int{}
	total, files := 0, 0
	for _, result := range results {
		if result.err != nil {
			continue
		}
		files++
		n := countCommentLines(result)
		total += n
		byLang[result.lang] += n
		byArea[area(result.rel)] += n
	}
	fmt.Fprintf(opts.stdout, "check-comments stats: %d files, %d comment lines\n", files, total)
	for _, key := range sortedKeys(byLang) {
		fmt.Fprintf(opts.stdout, "language %s: %d\n", key, byLang[key])
	}
	for _, key := range sortedKeys(byArea) {
		fmt.Fprintf(opts.stdout, "area %s: %d\n", key, byArea[key])
	}
	return 0, nil
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func area(rel string) string {
	switch {
	case strings.HasPrefix(rel, ".claude/hooks/"):
		return "hooks"
	case strings.HasPrefix(rel, "scripts/"):
		return "scripts"
	case strings.HasPrefix(rel, "agent/"):
		return "agent"
	case strings.HasPrefix(rel, "server/tests/"), strings.HasPrefix(rel, "server/") && (strings.HasSuffix(rel, "_test.go") || strings.Contains(rel, "/testutil/") || strings.Contains(rel, "/testpg/")):
		return "server-tests"
	case strings.HasPrefix(rel, "server/"):
		return "server-production"
	case strings.HasPrefix(rel, "web/"), strings.HasPrefix(rel, "tools/"):
		return "web-and-tools"
	case strings.HasPrefix(rel, "load/"):
		return "load"
	}
	return "workflows-deploy-policy-config"
}
