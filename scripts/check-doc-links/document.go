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
	atxHeadingPattern = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+(.+?)[ \t]*#*[ \t]*$`)
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
	return document{LineCount: lineCount, Anchors: parser.anchors}
}

type documentParser struct {
	anchors       map[string]struct{}
	headingCounts map[string]int
	fence         fenceState
}

func (p *documentParser) consume(lines [][]byte, index int) {
	line := string(lines[index])
	if p.fence.consume(line, true) || p.fence.active {
		return
	}
	p.addHTMLAnchors(line)
	p.addHeading(headingAt(lines, index))
}

func (p *documentParser) addHTMLAnchors(line string) {
	for _, match := range htmlAnchorPattern.FindAllStringSubmatch(line, -1) {
		p.anchors[html.UnescapeString(match[1])] = struct{}{}
	}
}

func (p *documentParser) addHeading(heading string) {
	baseAnchor := slugifyHeading(heading)
	if baseAnchor == "" {
		return
	}
	count := p.headingCounts[baseAnchor]
	p.headingCounts[baseAnchor] = count + 1
	if count > 0 {
		baseAnchor = fmt.Sprintf("%s-%d", baseAnchor, count)
	}
	p.anchors[baseAnchor] = struct{}{}
}

func headingAt(lines [][]byte, index int) string {
	line := string(lines[index])
	if match := atxHeadingPattern.FindStringSubmatch(line); match != nil {
		return match[1]
	}
	if index > 0 && setextPattern.MatchString(line) {
		return string(lines[index-1])
	}
	return ""
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
