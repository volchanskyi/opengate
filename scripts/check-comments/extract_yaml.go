package main

import (
	"regexp"
	"strings"
)

type yamlScanner struct {
	env       *env
	lang      string
	content   []byte
	idx       lineIndex
	lines     []string
	comments  []comment
	dataLines map[int]bool
	quote     byte
	flow      int
	template  bool
	tplStart  int
	tplOpen   string
}

var (
	tplOpening = regexp.MustCompile(`^\{\{-?\s*/\*`)
	tplClosing = regexp.MustCompile(`^\*/\s*-?\}\}`)
)

func (e *env) extractYAML(lang string, content []byte) ([]comment, map[int]bool, error) {
	s := &yamlScanner{env: e, lang: lang, content: content, idx: newLineIndex(content), dataLines: map[int]bool{}}
	s.lines = strings.Split(string(content), "\n")
	var block *blockScalar
	for i := 0; i < len(s.lines); i++ {
		line := strings.TrimRight(s.lines[i], "\r")
		if block != nil {
			if strings.TrimSpace(line) == "" || indentOf(line) > block.col {
				continue
			}
			if err := s.closeBlock(block, i); err != nil {
				return nil, nil, err
			}
			block = nil
		}
		code := s.scanLine(i, line)
		if s.lang != langTemplate {
			block = s.openBlock(i, code)
		}
	}
	if block != nil {
		if err := s.closeBlock(block, len(s.lines)); err != nil {
			return nil, nil, err
		}
	}
	return s.comments, s.dataLines, nil
}

func (s *yamlScanner) scanLine(i int, line string) string {
	base := s.idx.lineStart(i + 1)
	code := []byte(line)
	for j := 0; j < len(line); j++ {
		if s.template {
			if m := tplClosing.FindString(line[j:]); m != "" {
				s.comments = append(s.comments, comment{start: s.tplStart, end: base + j + len(m), open: s.tplOpen, close: m})
				blank(code, j, j+len(m))
				s.template = false
				j += len(m) - 1
				continue
			}
			code[j] = ' '
			continue
		}
		if s.lang == langHelm || s.lang == langTemplate {
			if m := tplOpening.FindString(line[j:]); m != "" {
				s.template, s.tplStart, s.tplOpen = true, base+j, m
				blank(code, j, j+len(m))
				j += len(m) - 1
				continue
			}
			if strings.HasPrefix(line[j:], "{{") {
				end := strings.Index(line[j:], "}}")
				if end < 0 {
					break
				}
				j += end + 1
				continue
			}
		}
		if s.lang == langTemplate {
			continue
		}
		if s.consumeQuoted(line, &j) {
			continue
		}
		c := line[j]
		if c == '#' && (j == 0 || line[j-1] == ' ' || line[j-1] == '\t') {
			s.comments = append(s.comments, comment{start: base + j, end: base + len(line), open: "#"})
			blank(code, j, len(line))
			break
		}
	}
	return strings.TrimRight(string(code), " \t")
}

func (s *yamlScanner) consumeQuoted(line string, j *int) bool {
	c := line[*j]
	switch s.quote {
	case '\'':
		if c == '\'' {
			if *j+1 < len(line) && line[*j+1] == '\'' {
				*j++
			} else {
				s.quote = 0
			}
		}
		return true
	case '"':
		if c == '\\' {
			*j++
		} else if c == '"' {
			s.quote = 0
		}
		return true
	}
	switch c {
	case '[', '{':
		s.flow++
	case ']', '}':
		if s.flow > 0 {
			s.flow--
		}
	case '\'', '"':
		if scalarStart(line, *j, s.flow > 0) {
			s.quote = c
			return true
		}
	}
	return false
}

func scalarStart(line string, j int, flow bool) bool {
	k := j - 1
	for k >= 0 && (line[k] == ' ' || line[k] == '\t') {
		k--
	}
	if k < 0 {
		return true
	}
	switch line[k] {
	case ':', '-', '[', '{', '?':
		return true
	case ',':
		return flow
	}
	return false
}

func blank(code []byte, from, to int) {
	for k := from; k < to && k < len(code); k++ {
		code[k] = ' '
	}
}
