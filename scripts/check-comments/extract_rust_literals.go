package main

import (
	"unicode"
	"unicode/utf8"
)

func (l *rustLexer) quoted(quote byte) {
	l.pos++
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case '\\':
			l.pos += 2
		case quote:
			l.pos++
			return
		default:
			l.pos++
		}
	}
}

func (l *rustLexer) raw() {
	hashes := 0
	for l.at(l.pos) == '#' {
		hashes++
		l.pos++
	}
	l.pos++
	for l.pos < len(l.src) {
		if l.src[l.pos] == '"' {
			closing := 0
			for closing < hashes && l.at(l.pos+1+closing) == '#' {
				closing++
			}
			if closing == hashes {
				l.pos += 1 + hashes
				return
			}
		}
		l.pos++
	}
}

func (l *rustLexer) startsRaw(offset int) bool {
	hashes := 0
	for l.at(offset+hashes) == '#' {
		hashes++
	}
	return l.at(offset+hashes) == '"'
}

func (l *rustLexer) quoteOrLifetime() {
	start := l.pos
	if l.at(l.pos+1) == '\\' {
		l.quoted('\'')
		l.emit(start)
		return
	}
	_, size := utf8.DecodeRune(l.src[l.pos+1:])
	if l.at(l.pos+1+size) == '\'' {
		l.pos += 2 + size
		l.emit(start)
		return
	}
	l.pos++
	for l.pos < len(l.src) && isRustIdentPart(l.src[l.pos]) {
		l.pos++
	}
	l.emit(start)
}

func (l *rustLexer) prefixedLiteral() bool {
	switch {
	case (l.hasPrefix("br") || l.hasPrefix("cr")) && l.startsRaw(l.pos+2):
		l.pos += 2
		l.raw()
	case l.hasPrefix("r") && l.startsRaw(l.pos+1):
		l.pos++
		l.raw()
	case (l.hasPrefix("b") || l.hasPrefix("c")) && l.at(l.pos+1) == '"':
		l.pos++
		l.quoted('"')
	case l.hasPrefix("b") && l.at(l.pos+1) == '\'':
		l.pos++
		l.quoted('\'')
	default:
		return false
	}
	return true
}

func (l *rustLexer) identOrPrefixed() {
	start := l.pos
	if l.prefixedLiteral() {
		l.emit(start)
		return
	}
	if l.hasPrefix("r#") {
		l.pos += 2
	}
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRune(l.src[l.pos:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		l.pos += size
	}
	l.emit(start)
}

func (l *rustLexer) number() {
	start := l.pos
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if isRustIdentPart(c) || (c == '.' && l.at(l.pos+1) >= '0' && l.at(l.pos+1) <= '9') {
			l.pos++
			continue
		}
		break
	}
	l.emit(start)
}
