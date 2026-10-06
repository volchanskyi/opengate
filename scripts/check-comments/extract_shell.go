package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type shellComment struct {
	line int
	col  int
}

func (e *env) shfmtTree(rel string, content []byte) (any, error) {
	command := exec.Command(e.shfmt, "--to-json", "--filename", rel)
	if e.root != "" {
		command.Dir = e.root
	}
	command.Stdin = bytes.NewReader(content)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("parse: %s", message)
	}
	var tree any
	if err := json.Unmarshal(stdout.Bytes(), &tree); err != nil {
		return nil, fmt.Errorf("parse: shfmt output: %w", err)
	}
	return tree, nil
}

func (e *env) extractShell(rel string, content []byte, withUnits bool) ([]comment, []string, error) {
	tree, err := e.shfmtTree(rel, content)
	if err != nil {
		return nil, nil, err
	}
	var found []shellComment
	collectShellComments(tree, &found)
	comments := shellComments(content, found, 0, 0)
	var units []string
	if withUnits {
		stripped, err := json.Marshal(stripPositions(tree))
		if err != nil {
			return nil, nil, err
		}
		units = []string{string(stripped)}
	}
	return comments, units, nil
}

func collectShellComments(node any, found *[]shellComment) {
	switch value := node.(type) {
	case map[string]any:
		if hash, ok := value["Hash"].(map[string]any); ok {
			line, _ := hash["Line"].(float64)
			col, _ := hash["Col"].(float64)
			*found = append(*found, shellComment{line: int(line), col: int(col)})
		}
		for _, child := range value {
			collectShellComments(child, found)
		}
	case []any:
		for _, child := range value {
			collectShellComments(child, found)
		}
	}
}

func shellComments(content []byte, found []shellComment, lineOffset, colOffset int) []comment {
	sort.Slice(found, func(i, j int) bool {
		if found[i].line != found[j].line {
			return found[i].line < found[j].line
		}
		return found[i].col < found[j].col
	})
	idx := newLineIndex(content)
	var comments []comment
	for _, item := range found {
		start := idx.lineStart(item.line+lineOffset) + colOffset + item.col - 1
		if start < 0 || start >= len(content) || content[start] != '#' {
			continue
		}
		end := bytes.IndexByte(content[start:], '\n')
		if end < 0 {
			end = len(content) - start
		}
		comments = append(comments, comment{start: start, end: start + end, open: "#"})
	}
	return comments
}

func stripPositions(node any) any {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			if key == "Comments" || key == "Last" || isPosition(child) {
				continue
			}
			out[key] = stripPositions(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, child := range value {
			out = append(out, stripPositions(child))
		}
		return out
	}
	return node
}

func isPosition(node any) bool {
	value, ok := node.(map[string]any)
	if !ok {
		return false
	}
	_, hasOffset := value["Offset"]
	_, hasLine := value["Line"]
	return hasOffset && hasLine
}
