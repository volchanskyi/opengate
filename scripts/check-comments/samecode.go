package main

import (
	"bytes"
	"sort"
	"strings"
)

type anchorLine struct {
	line int
	text string
}

func codeEqual(base, current fileResult) bool {
	if base.lang != current.lang || base.binary != current.binary {
		return false
	}
	if base.binary {
		return bytes.Equal(base.content, current.content)
	}
	if len(base.units) != len(current.units) {
		return false
	}
	for i := range base.units {
		if base.units[i] != current.units[i] {
			return false
		}
	}
	return true
}

func anchorLines(result fileResult) []anchorLine {
	var out []anchorLine
	for i, line := range strings.Split(codeText(result), "\n") {
		normal := strings.Join(strings.Fields(line), " ")
		if normal != "" {
			out = append(out, anchorLine{line: i + 1, text: normal})
		}
	}
	return out
}

func anchorsComparable(base, current fileResult) bool {
	left, right := anchorLines(base), anchorLines(current)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].text != right[i].text {
			return false
		}
	}
	return true
}

type commentPlace struct {
	startLine int
	anchor    int
}

func commentPlaces(result fileResult) []commentPlace {
	idx := newLineIndex(result.content)
	anchors := anchorLines(result)
	codeLines := strings.Split(codeText(result), "\n")
	var places []commentPlace
	for _, item := range result.comments {
		startLine := idx.line(item.start)
		endLine := idx.line(item.end - 1)
		lineStart := idx.lineStart(startLine)
		trailing := len(bytes.TrimSpace(result.content[lineStart:item.start])) > 0
		after := strings.TrimSpace(tail(codeLines, endLine, item.end-idx.lineStart(endLine)))
		target := endLine + 1
		if after != "" {
			target = endLine
		}
		if trailing {
			target = startLine
		}
		places = append(places, commentPlace{startLine: startLine, anchor: firstAnchorFrom(anchors, target)})
	}
	return places
}

func tail(lines []string, line, column int) string {
	if line-1 >= len(lines) {
		return ""
	}
	text := lines[line-1]
	if column >= len(text) {
		return ""
	}
	return text[column:]
}

func firstAnchorFrom(anchors []anchorLine, line int) int {
	return sort.Search(len(anchors), func(i int) bool { return anchors[i].line >= line })
}

func newComments(base, current fileResult) []int {
	allowed := map[int]bool{}
	for _, place := range commentPlaces(base) {
		allowed[place.anchor] = true
	}
	var added []int
	seen := map[int]bool{}
	for _, place := range commentPlaces(current) {
		if allowed[place.anchor] {
			continue
		}
		if !seen[place.startLine] {
			seen[place.startLine] = true
			added = append(added, place.startLine)
		}
	}
	sort.Ints(added)
	return added
}
