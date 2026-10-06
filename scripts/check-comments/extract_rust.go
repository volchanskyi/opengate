package main

import "unicode/utf8"

type rustLexer struct {
	src      []byte
	pos      int
	comments []comment
	units    []string
}

func extractRust(content []byte) ([]comment, []string) {
	lexer := &rustLexer{src: content}
	lexer.run()
	return lexer.comments, lexer.units
}

func (l *rustLexer) at(offset int) byte {
	if offset < len(l.src) {
		return l.src[offset]
	}
	return 0
}

func (l *rustLexer) hasPrefix(prefix string) bool {
	return len(l.src)-l.pos >= len(prefix) && string(l.src[l.pos:l.pos+len(prefix)]) == prefix
}

func (l *rustLexer) emit(start int) {
	l.units = append(l.units, string(l.src[start:l.pos]))
}

func (l *rustLexer) run() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case l.hasPrefix("//"):
			l.lineComment()
		case l.hasPrefix("/*"):
			l.blockComment()
		case c == '"':
			start := l.pos
			l.quoted('"')
			l.emit(start)
		case c == '\'':
			l.quoteOrLifetime()
		case isRustIdentStart(c):
			l.identOrPrefixed()
		case c >= '0' && c <= '9':
			l.number()
		default:
			start := l.pos
			_, size := utf8.DecodeRune(l.src[l.pos:])
			l.pos += size
			l.emit(start)
		}
	}
}

func (l *rustLexer) lineComment() {
	start := l.pos
	open := "//"
	if l.hasPrefix("///") && !l.hasPrefix("////") {
		open = "///"
	} else if l.hasPrefix("//!") {
		open = "//!"
	}
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
	l.comments = append(l.comments, comment{start: start, end: l.pos, open: open})
}

func (l *rustLexer) blockComment() {
	start := l.pos
	open := "/*"
	if l.hasPrefix("/**") && !l.hasPrefix("/**/") {
		open = "/**"
	} else if l.hasPrefix("/*!") {
		open = "/*!"
	}
	l.pos += 2
	depth := 1
	for l.pos < len(l.src) && depth > 0 {
		switch {
		case l.hasPrefix("/*"):
			depth++
			l.pos += 2
		case l.hasPrefix("*/"):
			depth--
			l.pos += 2
		default:
			l.pos++
		}
	}
	l.comments = append(l.comments, comment{start: start, end: l.pos, open: open, close: "*/"})
}

func isRustIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isRustIdentPart(c byte) bool {
	return isRustIdentStart(c) || (c >= '0' && c <= '9')
}
