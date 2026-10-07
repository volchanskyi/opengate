package main

import "bytes"

type textScanner struct {
	src      []byte
	pos      int
	idx      lineIndex
	comments []comment
	data     map[int]bool
}

func extractLineLanguage(lang string, content []byte) ([]comment, map[int]bool) {
	s := &textScanner{src: content, idx: newLineIndex(content), data: map[int]bool{}}
	switch lang {
	case langHash:
		s.lineStartComments([]string{"#", ";"}, false)
	case langDocker:
		s.lineStartComments([]string{"#"}, false)
	case langProperties:
		s.lineStartComments([]string{"#", "!"}, true)
	case langMake:
		s.makefile()
	case langTOML:
		s.toml()
	case langHCL:
		s.hcl()
	case langSQL:
		s.sql()
	case langRego:
		s.simple([]string{"#"}, nil, `"`, "`")
	case langPython:
		s.python()
	case langGoMod:
		s.simple([]string{"//"}, nil, `"`, "`")
	case langCSS:
		s.simple(nil, [][2]string{{"/*", "*/"}}, `"`, "'")
	case langHTML:
		s.html()
	}
	return s.comments, s.data
}

func (s *textScanner) has(prefix string) bool {
	return bytes.HasPrefix(s.src[s.pos:], []byte(prefix))
}

func (s *textScanner) lineEnd(from int) int {
	end := bytes.IndexByte(s.src[from:], '\n')
	if end < 0 {
		return len(s.src)
	}
	return from + end
}

func (s *textScanner) lineComment(open string) {
	end := s.lineEnd(s.pos)
	s.comments = append(s.comments, comment{start: s.pos, end: end, open: open})
	s.pos = end
}

func (s *textScanner) blockComment(open, close string, nested bool) {
	start := s.pos
	s.pos += len(open)
	depth := 1
	for s.pos < len(s.src) && depth > 0 {
		switch {
		case nested && s.has(open):
			depth++
			s.pos += len(open)
		case s.has(close):
			depth--
			s.pos += len(close)
		default:
			s.pos++
		}
	}
	s.comments = append(s.comments, comment{start: start, end: s.pos, open: open, close: close})
}

func (s *textScanner) quoted(open, close string, escape, doubled bool) {
	start := s.pos
	s.pos += len(open)
	for s.pos < len(s.src) {
		switch {
		case escape && s.src[s.pos] == '\\':
			s.pos += 2
		case doubled && s.has(close+close):
			s.pos += 2 * len(close)
		case s.has(close):
			s.pos += len(close)
			s.markData(start, s.pos)
			return
		default:
			s.pos++
		}
	}
	s.markData(start, s.pos)
}

func (s *textScanner) markData(from, to int) {
	first, last := s.idx.line(from), s.idx.line(to-1)
	for line := first + 1; line <= last; line++ {
		s.data[line] = true
	}
}

func (s *textScanner) simple(lines []string, blocks [][2]string, quotes ...string) {
	for s.pos < len(s.src) {
		if s.tryComments(lines, blocks, false) {
			continue
		}
		if quote, ok := s.matchAny(quotes); ok {
			s.quoted(quote, quote, quote != "`", false)
			continue
		}
		s.pos++
	}
}

func (s *textScanner) tryComments(lines []string, blocks [][2]string, nested bool) bool {
	for _, marker := range lines {
		if s.has(marker) {
			s.lineComment(marker)
			return true
		}
	}
	for _, pair := range blocks {
		if s.has(pair[0]) {
			s.blockComment(pair[0], pair[1], nested)
			return true
		}
	}
	return false
}

func (s *textScanner) matchAny(options []string) (string, bool) {
	for _, option := range options {
		if s.has(option) {
			return option, true
		}
	}
	return "", false
}
