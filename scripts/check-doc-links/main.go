// Command check-doc-links validates repository-local Markdown links without network access.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	root              string
	hookMode          bool
	baselinePath      string
	writeBaselinePath string
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
	flag.StringVar(&opts.baselinePath, "baseline", "", "suppress problem keys listed in this file")
	flag.StringVar(&opts.writeBaselinePath, "write-baseline", "", "write current problem keys to this file")
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
	if opts.writeBaselinePath != "" {
		return writeRequestedBaseline(linkChecker.root, opts, problems)
	}
	if opts.baselinePath != "" {
		problems, err = applyBaseline(linkChecker.root, opts.baselinePath, problems)
		if err != nil {
			return 0, err
		}
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

func writeRequestedBaseline(root string, opts options, problems []problem) (int, error) {
	if opts.hookMode {
		return 0, errors.New("--write-baseline cannot be used with --hook")
	}
	return 0, writeBaseline(root, opts.writeBaselinePath, problems)
}

func applyBaseline(root, path string, problems []problem) ([]problem, error) {
	baseline, err := readBaseline(root, path)
	if err != nil {
		return nil, err
	}
	return suppressBaseline(problems, baseline), nil
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

func suppressBaseline(problems []problem, baseline map[string]int) []problem {
	var unexpected []problem
	for _, item := range problems {
		key := problemFingerprint(item.key())
		if baseline[key] > 0 {
			baseline[key]--
			continue
		}
		unexpected = append(unexpected, item)
	}
	return unexpected
}

func readBaseline(root, path string) (map[string]int, error) {
	content, err := os.ReadFile(resolveAuxiliaryPath(root, path))
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	baseline := make(map[string]int)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fingerprint, count, parseErr := parseBaselineEntry(line)
		if parseErr != nil {
			return nil, parseErr
		}
		baseline[fingerprint] += count
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan baseline: %w", err)
	}
	return baseline, nil
}

func parseBaselineEntry(line string) (string, int, error) {
	countText, fingerprint, found := strings.Cut(line, "\t")
	if !found {
		return "", 0, fmt.Errorf("invalid baseline entry %q", line)
	}
	count, err := strconv.Atoi(countText)
	if err != nil || count < 1 {
		return "", 0, fmt.Errorf("invalid baseline count %q", countText)
	}
	decoded, err := hex.DecodeString(fingerprint)
	if err != nil || len(decoded) != sha256.Size {
		return "", 0, fmt.Errorf("invalid baseline fingerprint %q", fingerprint)
	}
	return fingerprint, count, nil
}

func writeBaseline(root, path string, problems []problem) error {
	counts := make(map[string]int, len(problems))
	for _, item := range problems {
		counts[problemFingerprint(item.key())]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	content := "# Existing Markdown link-debt fingerprints. New violations are still blocked.\n"
	for _, key := range keys {
		content += fmt.Sprintf("%d\t%s\n", counts[key], key)
	}
	if err := os.WriteFile(resolveAuxiliaryPath(root, path), []byte(content), 0o600); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

func problemFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func resolveAuxiliaryPath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, filepath.FromSlash(path))
}

func exitError(err error) {
	fmt.Fprintf(os.Stderr, "check-doc-links: %v\n", err)
	os.Exit(2)
}
