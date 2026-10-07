package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type sameCodeRun struct {
	root     string
	base     string
	noNew    bool
	declared map[string]bool
	loaded   scope
	env      *env
	out      io.Writer
	compared int
	differ   int
	added    int
}

func sameCodeMode(opts options, args []string) (int, error) {
	flags := flag.NewFlagSet("same-code", flag.ContinueOnError)
	flags.SetOutput(opts.stderr)
	noNew := flags.Bool("no-new-comments", false, "refuse a comment where BASE had none")
	declaredFile := flags.String("declared", "", "file listing paths whose code change is declared")
	if len(args) == 0 {
		return 0, prerequisiteError{"same-code needs a BASE ref"}
	}
	base := args[0]
	if err := flags.Parse(args[1:]); err != nil {
		return 2, nil
	}
	declared, err := readDeclared(*declaredFile)
	if err != nil {
		return 0, err
	}
	changed, err := changedFiles(opts.root, base, flags.Args())
	if err != nil {
		return 0, err
	}
	loaded, err := opts.loadScope(opts.root)
	if err != nil {
		return 0, prerequisiteError{err.Error()}
	}
	out := bufio.NewWriter(opts.stdout)
	defer out.Flush()
	r := &sameCodeRun{root: opts.root, base: base, noNew: *noNew, declared: declared, loaded: loaded, env: opts.env(opts.root), out: out}
	for _, rel := range changed {
		if err := r.check(rel); err != nil {
			return 0, err
		}
	}
	fmt.Fprintf(out, "same-code: %d files compared, %d differ, %d new comments\n", r.compared, r.differ, r.added)
	if r.differ > 0 || r.added > 0 {
		return 1, nil
	}
	return 0, nil
}

func (r *sameCodeRun) check(rel string) error {
	if r.loaded.excluded(rel) {
		return nil
	}
	lang, ok := classify(rel)
	if !ok {
		return nil
	}
	if err := checkPrerequisites(r.env, map[string]bool{lang: true}); err != nil {
		return err
	}
	r.compared++
	if r.declared[rel] {
		return nil
	}
	baseContent, inBase := gitShow(r.root, r.base, rel)
	current, err := os.ReadFile(filepath.Join(r.root, rel))
	if !inBase || err != nil {
		fmt.Fprintf(r.out, "%s: code differs (added or removed)\n", rel)
		r.differ++
		return nil
	}
	results, err := r.env.extractAll([]source{{rel: rel, content: baseContent}, {rel: rel, content: current}}, true)
	if err != nil {
		return err
	}
	if results[0].err != nil || results[1].err != nil || !codeEqual(results[0], results[1]) {
		fmt.Fprintf(r.out, "%s: code differs\n", rel)
		r.differ++
		return nil
	}
	if r.noNew {
		r.checkNewComments(rel, results[0], results[1])
	}
	return nil
}

func (r *sameCodeRun) checkNewComments(rel string, base, current fileResult) {
	if !anchorsComparable(base, current) {
		fmt.Fprintf(r.out, "%s: comment positions cannot be compared; the code lines moved\n", rel)
		r.added++
		return
	}
	for _, line := range newComments(base, current) {
		fmt.Fprintf(r.out, "%s:%d: new comment\n", rel, line)
		r.added++
	}
}

func readDeclared(file string) (map[string]bool, error) {
	declared := map[string]bool{}
	if file == "" {
		return declared, nil
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			declared[line] = true
		}
	}
	return declared, nil
}
