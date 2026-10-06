package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func hook(opts options, _ []string) (int, error) {
	plan, scoped, err := readHookPlan(opts.stdin, opts.root)
	if err != nil {
		return 0, err
	}
	if !scoped {
		return 0, nil
	}
	loaded, err := opts.loadScope(plan.root)
	if err == nil && loaded.excluded(plan.rel) {
		return 0, nil
	}
	lang, ok := classify(plan.rel)
	if !ok {
		return 0, nil
	}
	e := opts.env(plan.root)
	if _, statErr := os.Stat(filepath.Join(plan.root, "scripts", "lib", "tool-versions.sh")); statErr != nil {
		e.root = opts.root
	}
	if err := checkPrerequisites(e, map[string]bool{lang: true}); err != nil {
		return 0, err
	}
	results, err := e.extractAll([]source{{rel: plan.rel, content: plan.current}, {rel: plan.rel, content: plan.proposed}}, false)
	if err != nil {
		return 0, err
	}
	if results[1].err != nil {
		if results[0].err == nil {
			fmt.Fprintf(opts.stdout, "%s:1: %v\n", plan.rel, results[1].err)
			return 1, nil
		}
		return 0, nil
	}
	added := onlyNew(analyze(results[1]), analyze(results[0]))
	for _, item := range added {
		fmt.Fprintln(opts.stdout, item.String())
	}
	if len(added) > 0 {
		return 1, nil
	}
	return 0, nil
}
