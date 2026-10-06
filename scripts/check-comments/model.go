package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type source struct {
	rel     string
	content []byte
}

type comment struct {
	start int
	end   int
	open  string
	close string
}

type fileResult struct {
	rel       string
	lang      string
	content   []byte
	comments  []comment
	units     []string
	dataLines map[int]bool
	binary    bool
	err       error
}

type commentLine struct {
	Line      int
	Raw       string
	Body      string
	OwnLine   bool
	Directive bool
	ID        int
	Multi     bool
}

type violation struct {
	Path string
	Line int
	Code string
	Text string
}

func (v violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", v.Path, v.Line, v.Code, v.Text)
}

func (v violation) key() string {
	return v.Code + "\t" + v.Text
}

type lineIndex struct {
	starts []int
}

func newLineIndex(content []byte) lineIndex {
	starts := []int{0}
	for i, b := range content {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return lineIndex{starts: starts}
}

func (idx lineIndex) line(offset int) int {
	return sort.Search(len(idx.starts), func(i int) bool { return idx.starts[i] > offset })
}

func (idx lineIndex) lineStart(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(idx.starts) {
		return idx.starts[len(idx.starts)-1]
	}
	return idx.starts[line-1]
}

func (idx lineIndex) count() int {
	return len(idx.starts)
}

func sortComments(comments []comment) []comment {
	sort.SliceStable(comments, func(i, j int) bool { return comments[i].start < comments[j].start })
	return comments
}

func commentLines(result fileResult) []commentLine {
	if result.binary {
		return nil
	}
	idx := newLineIndex(result.content)
	var lines []commentLine
	for id, item := range result.comments {
		lines = append(lines, splitComment(result, idx, id, item)...)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Line < lines[j].Line })
	return lines
}

func splitComment(result fileResult, idx lineIndex, id int, item comment) []commentLine {
	content := result.content
	first := idx.line(item.start)
	prefix := content[idx.lineStart(first):item.start]
	own := len(bytes.TrimSpace(prefix)) == 0
	text := strings.ReplaceAll(string(content[item.start:item.end]), "\r", "")
	parts := strings.Split(text, "\n")
	directive := isDirective(first, text)
	var out []commentLine
	for i, part := range parts {
		raw := part
		if i > 0 {
			raw = strings.TrimLeft(part, " \t")
		}
		body := stripMarkers(item, raw, i, len(parts))
		out = append(out, commentLine{
			Line:      first + i,
			Raw:       strings.TrimRight(raw, " \t"),
			Body:      body,
			OwnLine:   own || i > 0,
			Directive: directive,
			ID:        id,
			Multi:     len(parts) > 1,
		})
	}
	return out
}

func stripMarkers(item comment, raw string, index, total int) string {
	text := raw
	if item.close == "" {
		text = strings.TrimPrefix(text, item.open)
		return trimBody(text)
	}
	if index == 0 {
		text = strings.TrimPrefix(text, item.open)
	}
	if index == total-1 {
		text = strings.TrimRight(text, " \t")
		text = strings.TrimSuffix(text, item.close)
	}
	if index > 0 {
		text = strings.TrimLeft(text, " \t")
		if strings.HasPrefix(text, "*") && !strings.HasPrefix(text, "*/") {
			text = text[1:]
		}
	}
	return trimBody(text)
}

func trimBody(text string) string {
	text = strings.TrimPrefix(text, " ")
	return strings.TrimRight(text, " \t")
}

func codeText(result fileResult) string {
	code := []byte(string(result.content))
	for _, item := range result.comments {
		for i := item.start; i < item.end && i < len(code); i++ {
			if code[i] != '\n' && code[i] != '\r' {
				code[i] = ' '
			}
		}
	}
	return string(code)
}

func runeWidth(text string) int {
	return utf8.RuneCountInString(text)
}

func splitLines(content []byte) []string {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func indentOf(text string) int {
	return len(text) - len(strings.TrimLeft(text, " \t"))
}
