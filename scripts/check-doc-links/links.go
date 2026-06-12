package main

import (
	"strings"
	"unicode"
)

func parseLinks(content []byte) []link {
	parser := linkParser{}
	for index, rawLine := range splitLines(content) {
		parser.consume(string(rawLine), index+1)
	}
	return parser.links
}

type linkParser struct {
	links     []link
	fence     fenceState
	inComment bool
}

func (p *linkParser) consume(line string, lineNumber int) {
	if p.fence.consume(line, !p.inComment) || p.fence.active {
		return
	}
	visibleLine := stripComments(line, &p.inComment)
	visibleLine = stripInlineCode(visibleLine)
	p.links = append(p.links, linksInLine(visibleLine, lineNumber)...)
}

func linksInLine(line string, lineNumber int) []link {
	var links []link
	for offset := 0; offset < len(line); {
		start, found := nextLinkStart(line, offset)
		if !found {
			break
		}
		end := findClosingParen(line, start)
		if end < 0 {
			break
		}
		destination := extractDestination(line[start:end])
		if destination != "" {
			links = append(links, link{Line: lineNumber, Destination: destination})
		}
		offset = end + 1
	}
	return links
}

func nextLinkStart(line string, offset int) (int, bool) {
	relativeStart := strings.Index(line[offset:], "](")
	if relativeStart < 0 {
		return 0, false
	}
	return offset + relativeStart + 2, true
}

type fenceState struct {
	active bool
	marker byte
}

func (state *fenceState) consume(line string, enabled bool) bool {
	if !enabled {
		return false
	}
	marker, found := fenceMarker(line)
	if !found {
		return false
	}
	if !state.active {
		state.active = true
		state.marker = marker
	} else if marker == state.marker {
		state.active = false
	}
	return true
}

func fenceMarker(line string) (byte, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return 0, false
	}
	if strings.HasPrefix(trimmed, "```") {
		return '`', true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return '~', true
	}
	return 0, false
}

func stripComments(line string, inComment *bool) string {
	var visible strings.Builder
	for len(line) > 0 {
		if *inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return visible.String()
			}
			line = line[end+3:]
			*inComment = false
			continue
		}

		start := strings.Index(line, "<!--")
		if start < 0 {
			visible.WriteString(line)
			return visible.String()
		}
		visible.WriteString(line[:start])
		line = line[start+4:]
		*inComment = true
	}
	return visible.String()
}

func stripInlineCode(line string) string {
	var visible strings.Builder
	for index := 0; index < len(line); {
		if line[index] != '`' {
			visible.WriteByte(line[index])
			index++
			continue
		}

		runLength := 1
		for index+runLength < len(line) && line[index+runLength] == '`' {
			runLength++
		}
		delimiter := strings.Repeat("`", runLength)
		end := strings.Index(line[index+runLength:], delimiter)
		if end < 0 {
			index += runLength
			continue
		}
		index += runLength + end + runLength
	}
	return visible.String()
}

func findClosingParen(value string, start int) int {
	state := parenState{depth: 1}
	for index := start; index < len(value); index++ {
		if state.consume(value[index]) {
			return index
		}
	}
	return -1
}

type parenState struct {
	depth   int
	escaped bool
}

func (state *parenState) consume(character byte) bool {
	if state.escaped {
		state.escaped = false
		return false
	}
	if character == '\\' {
		state.escaped = true
		return false
	}
	if character == '(' {
		state.depth++
	}
	if character == ')' {
		state.depth--
	}
	return state.depth == 0
}

func extractDestination(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if wrapped := angleWrappedDestination(value); wrapped != "" {
		return wrapped
	}
	if whitespace := firstUnescapedWhitespace(value); whitespace >= 0 {
		return value[:whitespace]
	}
	return value
}

func angleWrappedDestination(value string) string {
	if value[0] != '<' {
		return ""
	}
	end := strings.IndexByte(value, '>')
	if end < 1 {
		return ""
	}
	return value[1:end]
}

func firstUnescapedWhitespace(value string) int {
	escaped := false
	for index := 0; index < len(value); index++ {
		if escaped {
			escaped = false
			continue
		}
		if value[index] == '\\' {
			escaped = true
			continue
		}
		if unicode.IsSpace(rune(value[index])) {
			return index
		}
	}
	return -1
}
