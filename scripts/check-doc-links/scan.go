package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (c *checker) check() ([]problem, error) {
	markdownFiles, err := c.markdownFiles()
	if err != nil {
		return nil, err
	}

	var problems []problem
	for _, sourcePath := range markdownFiles {
		fileProblems, checkErr := c.checkFile(sourcePath)
		if checkErr != nil {
			return nil, checkErr
		}
		problems = append(problems, fileProblems...)
	}

	sortProblems(problems)
	return problems, nil
}

func (c *checker) checkFile(sourcePath string) ([]problem, error) {
	content, err := c.readFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sourcePath, err)
	}
	var problems []problem
	for _, parsedLink := range parseLinks(content) {
		issue := c.checkLink(sourcePath, parsedLink)
		if issue == "" {
			continue
		}
		problems = append(problems, problem{
			Source:  sourcePath,
			Line:    parsedLink.Line,
			Message: issue,
		})
	}
	return problems, nil
}

func sortProblems(problems []problem) {
	sort.Slice(problems, func(i, j int) bool {
		return problemLess(problems[i], problems[j])
	})
}

func problemLess(left, right problem) bool {
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Message < right.Message
}

func (c *checker) markdownFiles() ([]string, error) {
	fileSet := make(map[string]struct{})
	for _, directory := range []string{"docs", ".claude"} {
		if err := c.collectMarkdownDirectory(directory, fileSet); err != nil {
			return nil, fmt.Errorf("walk %s: %w", directory, err)
		}
	}

	addOverlayFiles(fileSet, c.overlays)
	return sortedPaths(fileSet), nil
}

func (c *checker) collectMarkdownDirectory(directory string, fileSet map[string]struct{}) error {
	walkRoot := filepath.Join(c.root, directory)
	err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		return c.collectMarkdownPath(path, entry, walkErr, fileSet)
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (c *checker) collectMarkdownPath(
	path string,
	entry fs.DirEntry,
	walkErr error,
	fileSet map[string]struct{},
) error {
	if walkErr != nil {
		return walkErr
	}
	if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
		return nil
	}
	relativePath, err := filepath.Rel(c.root, path)
	if err != nil {
		return err
	}
	fileSet[filepath.ToSlash(relativePath)] = struct{}{}
	return nil
}

func addOverlayFiles(fileSet map[string]struct{}, overlays map[string][]byte) {
	for relativePath := range overlays {
		if isScopedMarkdown(relativePath) {
			fileSet[relativePath] = struct{}{}
		}
	}
}

func sortedPaths(fileSet map[string]struct{}) []string {
	files := make([]string, 0, len(fileSet))
	for path := range fileSet {
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}

func isScopedMarkdown(relativePath string) bool {
	if !strings.EqualFold(filepath.Ext(relativePath), ".md") {
		return false
	}
	return relativePath == "docs" ||
		strings.HasPrefix(relativePath, "docs/") ||
		relativePath == ".claude" ||
		strings.HasPrefix(relativePath, ".claude/")
}

func (c *checker) readFile(relativePath string) ([]byte, error) {
	if content, ok := c.overlays[relativePath]; ok {
		return content, nil
	}
	return os.ReadFile(filepath.Join(c.root, filepath.FromSlash(relativePath)))
}
