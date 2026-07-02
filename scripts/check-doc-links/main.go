// Command check-doc-links validates repository-local Markdown links without network access.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// link records a Markdown destination and its source line.
type link struct {
	Line        int
	Destination string
}

// document caches the anchor and line metadata needed for target validation.
type document struct {
	LineCount int
	Anchors   map[string]struct{}
}

// problem describes one invalid repository-local Markdown link.
type problem struct {
	Source  string
	Line    int
	Message string
}

func (item problem) key() string {
	return item.Source + "\t" + item.Message
}

// checker resolves links against the repository plus an optional in-memory edit.
type checker struct {
	root      string
	overlays  map[string][]byte
	documents map[string]document
}

type options struct {
	root     string
	hookMode bool
}

func main() {
	status, err := execute(parseOptions())
	if err != nil {
		exitError(err)
	}
	os.Exit(status)
}

func parseOptions() options {
	var opts options
	flag.StringVar(&opts.root, "root", ".", "repository root")
	flag.BoolVar(&opts.hookMode, "hook", false, "read a PreToolUse envelope from stdin")
	flag.Parse()
	return opts
}

func execute(opts options) (int, error) {
	linkChecker, err := newChecker(opts.root)
	if err != nil {
		return 0, err
	}

	problems, err := collectProblems(&linkChecker, opts.hookMode)
	if err != nil {
		return 0, err
	}
	return reportProblems(problems), nil
}

func newChecker(root string) (checker, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return checker{}, fmt.Errorf("resolve root: %w", err)
	}
	return checker{
		root:      filepath.Clean(absoluteRoot),
		overlays:  make(map[string][]byte),
		documents: make(map[string]document),
	}, nil
}

func collectProblems(linkChecker *checker, hookMode bool) ([]problem, error) {
	if hookMode {
		return linkChecker.hookProblems(os.Stdin)
	}
	return linkChecker.check()
}

func (c *checker) hookProblems(reader io.Reader) ([]problem, error) {
	overlayPath, overlayContent, scoped, err := c.readHookOverlay(reader)
	if err != nil || !scoped {
		return nil, err
	}
	currentProblems, err := c.check()
	if err != nil {
		return nil, err
	}
	c.overlays[overlayPath] = overlayContent
	c.documents = make(map[string]document)
	proposedProblems, err := c.check()
	if err != nil {
		return nil, err
	}
	return onlyNewProblems(proposedProblems, currentProblems), nil
}

func reportProblems(problems []problem) int {
	for _, item := range problems {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", item.Source, item.Line, item.Message)
	}
	if len(problems) > 0 {
		return 1
	}
	return 0
}

// onlyNewProblems returns the problems present in proposed but not already in
// current — the diff that hook mode blocks. Pre-existing debt is never blocked;
// only newly introduced breakage is.
func onlyNewProblems(proposed, current []problem) []problem {
	currentCounts := make(map[string]int, len(current))
	for _, item := range current {
		currentCounts[item.key()]++
	}
	var added []problem
	for _, item := range proposed {
		key := item.key()
		if currentCounts[key] > 0 {
			currentCounts[key]--
			continue
		}
		added = append(added, item)
	}
	return added
}

func exitError(err error) {
	fmt.Fprintf(os.Stderr, "check-doc-links: %v\n", err)
	os.Exit(2)
}
