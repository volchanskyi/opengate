package main

import (
	"bytes"
	"regexp"
)

var dollarTag = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

func (s *textScanner) toml() {
	for s.pos < len(s.src) {
		switch {
		case s.has(`"""`):
			s.quoted(`"""`, `"""`, true, false)
		case s.has(`'''`):
			s.quoted(`'''`, `'''`, false, false)
		case s.has(`"`):
			s.quoted(`"`, `"`, true, false)
		case s.has(`'`):
			s.quoted(`'`, `'`, false, false)
		case s.has("#"):
			s.lineComment("#")
		default:
			s.pos++
		}
	}
}

func (s *textScanner) sql() {
	for s.pos < len(s.src) {
		switch {
		case s.tryComments([]string{"--"}, [][2]string{{"/*", "*/"}}, true):
		case s.has("'"):
			s.quoted("'", "'", false, true)
		case s.has(`"`):
			s.quoted(`"`, `"`, false, true)
		case s.src[s.pos] == '$' && (s.pos == 0 || !isRustIdentPart(s.src[s.pos-1])):
			if tag := dollarTag.Find(s.src[s.pos:]); tag != nil {
				s.quoted(string(tag), string(tag), false, false)
			} else {
				s.pos++
			}
		default:
			s.pos++
		}
	}
}

func (s *textScanner) python() {
	for s.pos < len(s.src) {
		switch {
		case s.has(`"""`) || s.has(`'''`):
			quote := string(s.src[s.pos : s.pos+3])
			s.quoted(quote, quote, true, false)
		case s.has(`"`) || s.has(`'`):
			quote := string(s.src[s.pos : s.pos+1])
			s.quoted(quote, quote, true, false)
		case s.has("#"):
			s.lineComment("#")
		default:
			s.pos++
		}
	}
}

func (s *textScanner) html() {
	lower := bytes.ToLower(s.src)
	for s.pos < len(s.src) {
		switch {
		case s.has("<!--"):
			s.blockComment("<!--", "-->", false)
		case bytes.HasPrefix(lower[s.pos:], []byte("<script")):
			s.skipElement(lower, "</script>")
		case bytes.HasPrefix(lower[s.pos:], []byte("<style")):
			s.skipElement(lower, "</style>")
		default:
			s.pos++
		}
	}
}

func (s *textScanner) skipElement(lower []byte, closing string) {
	end := bytes.Index(lower[s.pos:], []byte(closing))
	if end < 0 {
		s.pos = len(s.src)
		return
	}
	s.pos += end + len(closing)
}
