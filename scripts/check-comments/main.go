package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type options struct {
	root       string
	typescript string
	scopeFile  string
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
}

type prerequisiteError struct {
	message string
}

func (e prerequisiteError) Error() string {
	return e.message
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check-comments", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := options{stdin: stdin, stdout: stdout, stderr: stderr}
	flags.StringVar(&opts.root, "root", ".", "repository root")
	flags.StringVar(&opts.typescript, "typescript", "", "TypeScript compiler directory")
	flags.StringVar(&opts.scopeFile, "scope", "", "out-of-scope list")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	root, err := filepath.Abs(opts.root)
	if err != nil {
		return fail(stderr, err)
	}
	opts.root = root
	if opts.typescript == "" {
		opts.typescript = filepath.Join(root, "web", "node_modules", "typescript")
	}
	rest := flags.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "usage: check-comments [--root DIR] [--typescript DIR] [--scope FILE] sweep [PATH...] | hook | same-code BASE [--no-new-comments] [--declared FILE] [PATH...] | stats [PATH...]")
		return 2
	}
	modes := map[string]func(options, []string) (int, error){
		"sweep":     sweep,
		"hook":      hook,
		"same-code": sameCodeMode,
		"stats":     stats,
	}
	mode, ok := modes[rest[0]]
	if !ok {
		fmt.Fprintf(stderr, "check-comments: unknown mode %q\n", rest[0])
		return 2
	}
	status, err := mode(opts, rest[1:])
	if err != nil {
		return fail(stderr, err)
	}
	return status
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "check-comments: %v\n", err)
	return 2
}

func (o options) env(root string) *env {
	return &env{root: root, typescript: o.typescript, shfmt: "shfmt"}
}

func (o options) loadScope(root string) (scope, error) {
	file := o.scopeFile
	if file == "" {
		file = filepath.Join(root, "scripts", "check-comments", "scope.tsv")
		if _, err := os.Stat(file); err != nil {
			file = filepath.Join(o.root, "scripts", "check-comments", "scope.tsv")
		}
	}
	return loadScope(file)
}
