package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	lineAnchorPattern = regexp.MustCompile(`(?i)^L([0-9]+)(?:-L?([0-9]+))?$`)
	schemePattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

func (c *checker) checkLink(sourcePath string, parsedLink link) string {
	destination := strings.TrimSpace(parsedLink.Destination)
	if destination == "" || isExternal(destination) {
		return ""
	}

	pathPart, fragment := splitDestination(destination)
	targetPath, issue := c.checkTarget(sourcePath, pathPart)
	if issue != "" {
		return issue
	}
	if isActivePlan(targetPath) && targetPath != sourcePath {
		return fmt.Sprintf("links to active plan %q; only .claude/plans/archive/ is allowed", destination)
	}
	info, exists, err := c.targetInfo(targetPath)
	if err != nil {
		return fmt.Sprintf("inspect target %q: %v", destination, err)
	}
	if !exists {
		return fmt.Sprintf("target does not exist: %q", destination)
	}
	if fragment == "" {
		return ""
	}
	return c.checkAnchor(targetPath, destination, fragment, info)
}

func (c *checker) checkTarget(sourcePath, pathPart string) (string, string) {
	targetPath, err := c.resolveTarget(sourcePath, pathPart)
	if err != nil {
		return "", err.Error()
	}
	return targetPath, ""
}

func (c *checker) checkAnchor(targetPath, destination, fragment string, info fs.FileInfo) string {
	if info.IsDir() {
		return fmt.Sprintf("anchor %q cannot target directory %q", "#"+fragment, destination)
	}

	targetDocument, err := c.document(targetPath)
	if err != nil {
		return fmt.Sprintf("read anchor target %q: %v", destination, err)
	}
	if match := lineAnchorPattern.FindStringSubmatch(fragment); match != nil {
		return checkLineAnchor(targetPath, fragment, match, targetDocument.LineCount)
	}

	return checkHeadingAnchor(targetPath, fragment, targetDocument)
}

func checkLineAnchor(targetPath, fragment string, match []string, lineCount int) string {
	requestedLine, _ := strconv.Atoi(match[1])
	if match[2] != "" {
		endLine, _ := strconv.Atoi(match[2])
		requestedLine = max(requestedLine, endLine)
	}
	if requestedLine > lineCount {
		return fmt.Sprintf("line anchor %q exceeds %d lines in %q", "#"+fragment, lineCount, targetPath)
	}
	return ""
}

func checkHeadingAnchor(targetPath, fragment string, targetDocument document) string {
	decodedFragment, decodeErr := url.PathUnescape(fragment)
	if decodeErr != nil {
		return fmt.Sprintf("invalid anchor %q: %v", "#"+fragment, decodeErr)
	}
	if _, ok := targetDocument.Anchors[decodedFragment]; !ok {
		return fmt.Sprintf("heading anchor %q not found in %q", "#"+decodedFragment, targetPath)
	}
	return ""
}

func isExternal(destination string) bool {
	return strings.HasPrefix(destination, "//") ||
		strings.HasPrefix(destination, "/") ||
		schemePattern.MatchString(destination)
}

func splitDestination(destination string) (string, string) {
	if hash := strings.IndexByte(destination, '#'); hash >= 0 {
		return stripQuery(destination[:hash]), destination[hash+1:]
	}
	return stripQuery(destination), ""
}

func stripQuery(path string) string {
	if query := strings.IndexByte(path, '?'); query >= 0 {
		return path[:query]
	}
	return path
}

func (c *checker) resolveTarget(sourcePath, destinationPath string) (string, error) {
	if destinationPath == "" {
		return sourcePath, nil
	}

	decodedPath, err := url.PathUnescape(destinationPath)
	if err != nil {
		return "", fmt.Errorf("invalid target path %q: %w", destinationPath, err)
	}
	decodedPath = strings.ReplaceAll(decodedPath, `\ `, " ")
	target := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), filepath.FromSlash(decodedPath)))
	target = filepath.ToSlash(target)
	if target == ".." || strings.HasPrefix(target, "../") {
		return "", fmt.Errorf("target escapes repository: %q", destinationPath)
	}
	return target, nil
}

func isActivePlan(targetPath string) bool {
	const plansPrefix = ".claude/plans/"
	if !strings.HasPrefix(targetPath, plansPrefix) {
		return false
	}
	return !strings.HasPrefix(targetPath, plansPrefix+"archive/")
}

func (c *checker) targetInfo(relativePath string) (fs.FileInfo, bool, error) {
	if _, ok := c.overlays[relativePath]; ok {
		return overlayFileInfo{name: filepath.Base(relativePath)}, true, nil
	}
	info, err := os.Stat(filepath.Join(c.root, filepath.FromSlash(relativePath)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return info, true, nil
}

func (c *checker) document(relativePath string) (document, error) {
	if cached, ok := c.documents[relativePath]; ok {
		return cached, nil
	}
	content, err := c.readFile(relativePath)
	if err != nil {
		return document{}, err
	}
	parsed := parseDocument(content, strings.EqualFold(filepath.Ext(relativePath), ".md"))
	c.documents[relativePath] = parsed
	return parsed, nil
}

type overlayFileInfo struct {
	name string
}

func (info overlayFileInfo) Name() string  { return info.name }
func (overlayFileInfo) Size() int64        { return 0 }
func (overlayFileInfo) Mode() fs.FileMode  { return 0 }
func (overlayFileInfo) ModTime() time.Time { return time.Time{} }
func (overlayFileInfo) IsDir() bool        { return false }
func (overlayFileInfo) Sys() any           { return nil }
