package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf8"
)

type scriptRequest struct {
	Files []scriptFile `json:"files"`
	Units bool         `json:"units"`
}

type scriptFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type scriptResult struct {
	Comments [][2]int `json:"comments"`
	Units    []string `json:"units"`
}

func (e *env) extractScripts(results []fileResult, indexes []int, withUnits bool) error {
	request := scriptRequest{Units: withUnits}
	for _, i := range indexes {
		request.Files = append(request.Files, scriptFile{Path: results[i].rel, Content: string(results[i].content)})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	command := exec.Command("node", "-e", typescriptScript, e.typescript)
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("TypeScript extraction: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var replies []scriptResult
	if err := json.Unmarshal(stdout.Bytes(), &replies); err != nil {
		return fmt.Errorf("TypeScript extraction output: %w", err)
	}
	if len(replies) != len(indexes) {
		return fmt.Errorf("TypeScript extraction returned %d files for %d", len(replies), len(indexes))
	}
	for n, i := range indexes {
		offsets := utf16Offsets(results[i].content)
		jsx := strings.HasSuffix(results[i].rel, ".tsx") || strings.HasSuffix(results[i].rel, ".jsx")
		for _, pair := range replies[n].Comments {
			start, end := offsets[pair[0]], offsets[pair[1]]
			results[i].comments = append(results[i].comments, scriptComment(results[i].content, start, end, jsx))
		}
		results[i].comments = sortComments(results[i].comments)
		results[i].units = replies[n].Units
	}
	return nil
}

func scriptComment(content []byte, start, end int, jsx bool) comment {
	if bytes.HasPrefix(content[start:], []byte("/*")) {
		open := "/*"
		if bytes.HasPrefix(content[start:], []byte("/**")) && !bytes.HasPrefix(content[start:], []byte("/**/")) {
			open = "/**"
		}
		if jsx {
			if braced, ok := jsxBraces(content, start, end, open); ok {
				return braced
			}
		}
		return comment{start: start, end: end, open: open, close: "*/"}
	}
	open := "//"
	if bytes.HasPrefix(content[start:], []byte("///")) {
		open = "///"
	}
	return comment{start: start, end: end, open: open}
}

func jsxBraces(content []byte, start, end int, open string) (comment, bool) {
	before := start - 1
	for before >= 0 && (content[before] == ' ' || content[before] == '\t') {
		before--
	}
	after := end
	for after < len(content) && (content[after] == ' ' || content[after] == '\t') {
		after++
	}
	if before < 0 || content[before] != '{' || after >= len(content) || content[after] != '}' {
		return comment{}, false
	}
	return comment{
		start: before,
		end:   after + 1,
		open:  string(content[before:start]) + open,
		close: "*/" + string(content[end:after+1]),
	}, true
}

func utf16Offsets(content []byte) []int {
	offsets := make([]int, 0, len(content)+1)
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRune(content[i:])
		offsets = append(offsets, i)
		if r >= 0x10000 {
			offsets = append(offsets, i)
		}
		i += size
	}
	return append(offsets, len(content))
}
