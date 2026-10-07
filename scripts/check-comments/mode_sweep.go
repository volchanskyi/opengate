package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type inScope struct {
	sources []source
	unknown []string
	langs   map[string]bool
}

func collect(opts options, paths []string, checkStale bool) (inScope, []string, error) {
	files, err := listFiles(opts.root, paths)
	if err != nil {
		return inScope{}, nil, err
	}
	loaded, err := opts.loadScope(opts.root)
	if err != nil {
		return inScope{}, nil, prerequisiteError{err.Error()}
	}
	var stale []string
	if checkStale {
		stale = loaded.unmatched(files)
	}
	found := inScope{langs: map[string]bool{}}
	for _, rel := range files {
		if loaded.excluded(rel) {
			continue
		}
		lang, ok := classify(rel)
		if !ok {
			found.unknown = append(found.unknown, rel)
			continue
		}
		content, err := os.ReadFile(filepath.Join(opts.root, rel))
		if err != nil {
			return inScope{}, nil, err
		}
		found.langs[lang] = true
		found.sources = append(found.sources, source{rel: rel, content: content})
	}
	return found, stale, nil
}

func extractScoped(opts options, paths []string, checkStale bool) (inScope, []string, []fileResult, error) {
	found, stale, err := collect(opts, paths, checkStale)
	if err != nil {
		return found, nil, nil, err
	}
	e := opts.env(opts.root)
	if err := checkPrerequisites(e, found.langs); err != nil {
		return found, nil, nil, err
	}
	results, err := e.extractAll(found.sources, false)
	return found, stale, results, err
}

func sweep(opts options, paths []string) (int, error) {
	found, stale, results, err := extractScoped(opts, paths, len(paths) == 0)
	if err != nil {
		return 0, err
	}
	out := bufio.NewWriter(opts.stdout)
	defer out.Flush()
	failed := reportInventory(out, stale, found.unknown)
	count, commentCount, violations := 0, 0, 0
	for _, result := range results {
		if result.err != nil {
			fmt.Fprintf(out, "%s:1: %v\n", result.rel, result.err)
			failed = true
			continue
		}
		count++
		commentCount += countCommentLines(result)
		for _, item := range analyze(result) {
			fmt.Fprintln(out, item.String())
			violations++
			failed = true
		}
	}
	if count == 0 {
		fmt.Fprintln(out, "check-comments: read no files")
		return 1, nil
	}
	fmt.Fprintf(out, "check-comments: read %d files, %d comment lines, %d violations\n", count, commentCount, violations)
	if failed {
		return 1, nil
	}
	return 0, nil
}

func reportInventory(out io.Writer, stale, unknown []string) bool {
	for _, entry := range stale {
		fmt.Fprintf(out, "scope entry %q matches no file\n", entry)
	}
	for _, rel := range unknown {
		fmt.Fprintf(out, "%s: unknown file type; classify it in scripts/check-comments or list it in scope.tsv with a reason\n", rel)
	}
	return len(stale) > 0 || len(unknown) > 0
}

func countCommentLines(result fileResult) int {
	seen := map[int]bool{}
	for _, line := range commentLines(result) {
		if !line.Directive {
			seen[line.Line] = true
		}
	}
	return len(seen)
}
