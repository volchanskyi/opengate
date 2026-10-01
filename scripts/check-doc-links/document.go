package main

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
)

var (
	atxHeadingPattern = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)
	setextPattern     = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	htmlAnchorPattern = regexp.MustCompile(`(?i)<a[ \t]+[^>]*(?:id|name)=["']([^"']+)["'][^>]*>`)
	htmlTagPattern    = regexp.MustCompile(`<[^>]+>`)
)

func parseDocument(content []byte, markdown bool) document {
	lineCount := countLines(content)
	if !markdown {
		return document{LineCount: lineCount, Anchors: make(map[string]struct{})}
	}

	lines := splitLines(content)
	parser := documentParser{
		anchors:       make(map[string]struct{}),
		headingCounts: make(map[string]int),
	}
	for index := range lines {
		parser.consume(lines, index)
	}
	return document{LineCount: lineCount, Anchors: parser.anchors, Headings: parser.headings}
}

type documentParser struct {
	anchors       map[string]struct{}
	headingCounts map[string]int
	headings      []heading
	fence         fenceState
}

func (p *documentParser) consume(lines [][]byte, index int) {
	line := string(lines[index])
	if p.fence.consume(line, true) || p.fence.active {
		return
	}
	p.addHTMLAnchors(line)
	text, level := headingAt(lines, index)
	p.addHeading(index+1, level, text)
}

func (p *documentParser) addHTMLAnchors(line string) {
	for _, match := range htmlAnchorPattern.FindAllStringSubmatch(line, -1) {
		p.anchors[html.UnescapeString(match[1])] = struct{}{}
	}
}

func (p *documentParser) addHeading(lineNumber, level int, text string) {
	anchor := slugifyHeading(text)
	if anchor == "" {
		return
	}
	count := p.headingCounts[anchor]
	p.headingCounts[anchor] = count + 1
	if count > 0 {
		anchor = fmt.Sprintf("%s-%d", anchor, count)
	}
	p.anchors[anchor] = struct{}{}
	p.headings = append(p.headings, heading{Line: lineNumber, Level: level, Text: text, Anchor: anchor})
}

// headingAt returns the text and level of the heading on a line, or "" when the
// line is none.
func headingAt(lines [][]byte, index int) (string, int) {
	line := string(lines[index])
	if match := atxHeadingPattern.FindStringSubmatch(line); match != nil {
		return match[2], len(match[1])
	}
	if index > 0 && setextPattern.MatchString(line) {
		level := 2
		if strings.HasPrefix(strings.TrimSpace(line), "=") {
			level = 1
		}
		return string(lines[index-1]), level
	}
	return "", 0
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

func splitLines(content []byte) [][]byte {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	return bytes.Split(content, []byte{'\n'})
}

func slugifyHeading(heading string) string {
	heading = removeLinkDestinations(stripInlineCodeMarkers(heading))
	heading = html.UnescapeString(htmlTagPattern.ReplaceAllString(heading, ""))
	heading = strings.ToLower(heading)

	var slug strings.Builder
	for _, character := range heading {
		switch {
		case unicode.IsLetter(character), unicode.IsNumber(character), character == '-', character == '_':
			slug.WriteRune(character)
		case unicode.IsSpace(character):
			slug.WriteByte('-')
		}
	}
	return strings.Trim(slug.String(), "-")
}

func stripInlineCodeMarkers(value string) string {
	return strings.ReplaceAll(value, "`", "")
}

func removeLinkDestinations(value string) string {
	for {
		start := strings.Index(value, "](")
		if start < 0 {
			return value
		}
		end := findClosingParen(value, start+2)
		if end < 0 {
			return value
		}
		value = value[:start+1] + value[end+1:]
	}
}
