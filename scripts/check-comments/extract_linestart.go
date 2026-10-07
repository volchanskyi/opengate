package main

import "strings"

func (s *textScanner) lineStartComments(markers []string, continuation bool) {
	lines := strings.Split(string(s.src), "\n")
	offset := 0
	continued := false
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		start := offset + len(line) - len(trimmed)
		if !continued {
			for _, marker := range markers {
				if strings.HasPrefix(trimmed, marker) {
					end := offset + len(strings.TrimRight(line, "\r"))
					s.comments = append(s.comments, comment{start: start, end: end, open: marker})
					break
				}
			}
		}
		body := strings.TrimRight(line, "\r")
		continued = continuation && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "!") && oddBackslashes(body)
		offset += len(line) + 1
	}
}

func oddBackslashes(line string) bool {
	count := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

func (s *textScanner) makefile() {
	lines := strings.Split(string(s.src), "\n")
	offset := 0
	for _, line := range lines {
		body := strings.TrimRight(line, "\r")
		if strings.HasPrefix(body, "\t") {
			s.recipeComment(offset, body)
		} else if at := makeCommentAt(body); at >= 0 {
			s.comments = append(s.comments, comment{start: offset + at, end: offset + len(body), open: "#"})
		}
		offset += len(line) + 1
	}
}

func makeCommentAt(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '#' {
			return i
		}
	}
	return -1
}

func (s *textScanner) recipeComment(offset int, line string) {
	rest := strings.TrimLeft(line[1:], "@+- \t")
	lead := len(line) - len(rest)
	if strings.HasPrefix(rest, "#") {
		first := 1
		for first < len(line) && (line[first] == ' ' || line[first] == '\t') {
			first++
		}
		s.comments = append(s.comments, comment{start: offset + first, end: offset + len(line), open: line[first : lead+1]})
		return
	}
	var quote byte
	for i := 1; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case quote == '"':
			if c == '\\' {
				i++
			} else if c == '"' {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (line[i-1] == ' ' || line[i-1] == '\t'):
			s.comments = append(s.comments, comment{start: offset + i, end: offset + len(line), open: "#"})
			return
		}
	}
}
