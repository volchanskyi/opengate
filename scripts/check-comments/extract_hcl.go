package main

import (
	"bytes"
	"regexp"
)

var heredocMarker = regexp.MustCompile(`^<<-?([A-Za-z_][A-Za-z0-9_]*)[ \t]*\r?$`)

func (s *textScanner) hcl() {
	for s.pos < len(s.src) {
		switch {
		case s.has("<<") && heredocMarker.Match(s.src[s.pos:s.lineEnd(s.pos)]):
			s.heredoc()
		case s.has(`"`):
			s.hclString()
		case s.tryComments([]string{"#", "//"}, [][2]string{{"/*", "*/"}}, false):
		default:
			s.pos++
		}
	}
}

func (s *textScanner) heredoc() {
	start := s.pos
	marker := heredocMarker.FindSubmatch(s.src[s.pos:s.lineEnd(s.pos)])[1]
	s.pos = s.lineEnd(s.pos)
	for s.pos < len(s.src) {
		next := s.lineEnd(s.pos + 1)
		line := bytes.TrimSpace(s.src[s.pos+1 : next])
		s.pos = next
		if bytes.Equal(line, marker) {
			break
		}
	}
	s.markData(start, s.pos)
}

func (s *textScanner) hclString() {
	start := s.pos
	s.pos++
	for s.pos < len(s.src) {
		switch {
		case s.src[s.pos] == '\\':
			s.pos += 2
		case s.src[s.pos] == '"':
			s.pos++
			s.markData(start, s.pos)
			return
		case s.has("${") || s.has("%{"):
			s.pos += 2
			s.interpolation()
		default:
			s.pos++
		}
	}
}

func (s *textScanner) interpolation() {
	depth := 1
	for s.pos < len(s.src) && depth > 0 {
		switch s.src[s.pos] {
		case '{':
			depth++
			s.pos++
		case '}':
			depth--
			s.pos++
		case '"':
			s.hclString()
		default:
			s.pos++
		}
	}
}
