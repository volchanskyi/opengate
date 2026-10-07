package main

import (
	"regexp"
	"strings"
)

var (
	goTestDecl     = regexp.MustCompile(`^func (Test|Benchmark|Fuzz)[A-Za-z0-9_]*\(`)
	rustTestAttr   = regexp.MustCompile(`^\s*#\[(tokio::)?test\b`)
	rustAttribute  = regexp.MustCompile(`^\s*#\[.*\]\s*$`)
	rustFn         = regexp.MustCompile(`\bfn\b`)
	scriptTestDecl = regexp.MustCompile("^\\s*(describe|it|test)(\\.(describe|each|only|skip|concurrent|serial|parallel|fixme|fail|todo))*\\s*[(`]")
	scriptTestFile = regexp.MustCompile(`\.(test|spec)\.[cm]?[jt]sx?$`)
)

func testDocLines(result fileResult, lines []commentLine) []commentLine {
	codeLines := strings.Split(codeText(result), "\n")
	byLine := map[int][]commentLine{}
	for _, line := range lines {
		if !line.Directive {
			byLine[line.Line] = append(byLine[line.Line], line)
		}
	}
	blocks := groupBlocks(lines)
	blockAt := map[int][]commentLine{}
	for _, block := range blocks {
		for _, line := range block {
			blockAt[line.Line] = block
		}
	}
	var flagged []commentLine
	flagAbove := func(decl int) {
		if block, ok := blockAt[decl-1]; ok && block[len(block)-1].OwnLine {
			if first, ok := firstProse(block); ok {
				flagged = append(flagged, first)
			}
		}
	}
	flagOn := func(line int) {
		for _, item := range byLine[line] {
			if !item.OwnLine {
				flagged = append(flagged, item)
			}
		}
	}
	switch {
	case result.lang == langGo && strings.HasSuffix(result.rel, "_test.go"):
		for i, code := range codeLines {
			if goTestDecl.MatchString(code) {
				flagAbove(i + 1)
				flagOn(i + 1)
			}
		}
	case result.lang == langRust:
		flagged = append(flagged, rustTestDocs(codeLines, byLine, blockAt)...)
	case result.lang == langTS && scriptTestFile.MatchString(result.rel):
		for i, code := range codeLines {
			if scriptTestDecl.MatchString(code) {
				flagAbove(i + 1)
				flagOn(i + 1)
			}
		}
	}
	return flagged
}

func firstProse(block []commentLine) (commentLine, bool) {
	for _, line := range block {
		if !line.Directive {
			return line, true
		}
	}
	return commentLine{}, false
}

func rustTestDocs(codeLines []string, byLine map[int][]commentLine, blockAt map[int][]commentLine) []commentLine {
	var flagged []commentLine
	flag := func(line int) {
		if block, ok := blockAt[line]; ok {
			if first, ok := firstProse(block); ok {
				flagged = append(flagged, first)
			}
		}
	}
	for i, code := range codeLines {
		if !rustTestAttr.MatchString(code) {
			continue
		}
		for up := i - 1; up >= 0; up-- {
			if rustAttribute.MatchString(codeLines[up]) {
				continue
			}
			if strings.TrimSpace(codeLines[up]) == "" && len(byLine[up+1]) > 0 {
				flag(up + 1)
				continue
			}
			break
		}
		for down := i; down < len(codeLines); down++ {
			if len(byLine[down+1]) > 0 {
				flag(down + 1)
			}
			if rustFn.MatchString(codeLines[down]) {
				break
			}
		}
	}
	return flagged
}
