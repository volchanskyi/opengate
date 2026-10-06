package main

import (
	"regexp"
	"strings"
)

var listHeader = regexp.MustCompile(`^(Usage|Environment|Exit codes):|^JUSTIFICATIONS\b`)

func groupBlocks(lines []commentLine) [][]commentLine {
	var blocks [][]commentLine
	var current []commentLine
	for _, line := range lines {
		if len(current) > 0 && joinsBlock(current[len(current)-1], line) {
			current = append(current, line)
			continue
		}
		if len(current) > 0 {
			blocks = append(blocks, current)
		}
		current = []commentLine{line}
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

func joinsBlock(previous, line commentLine) bool {
	if previous.ID == line.ID {
		return true
	}
	eligible := func(item commentLine) bool { return item.OwnLine || item.Multi }
	if !eligible(previous) || !line.OwnLine {
		return false
	}
	return line.Line == previous.Line || line.Line == previous.Line+1
}

const (
	kindProse = iota
	kindEmpty
	kindExcluded
)

func blockLength(block []commentLine) int {
	var kinds []int
	var bodies []string
	lastLine := -1
	for _, line := range block {
		if line.Line == lastLine {
			continue
		}
		lastLine = line.Line
		bodies = append(bodies, line.Body)
		switch {
		case line.Directive:
			kinds = append(kinds, kindExcluded)
		case strings.TrimSpace(line.Body) == "":
			kinds = append(kinds, kindEmpty)
		default:
			kinds = append(kinds, kindProse)
		}
	}
	markLists(kinds, bodies)
	total := 0
	var run []int
	flush := func() {
		start, end := 0, len(run)
		for start < end && run[start] == kindEmpty {
			start++
		}
		for end > start && run[end-1] == kindEmpty {
			end--
		}
		total += end - start
		run = nil
	}
	for _, kind := range kinds {
		if kind == kindExcluded {
			flush()
			continue
		}
		run = append(run, kind)
	}
	flush()
	return total
}

func markLists(kinds []int, bodies []string) {
	for i := 0; i < len(kinds); i++ {
		if kinds[i] != kindProse || !listHeader.MatchString(strings.TrimLeft(bodies[i], " ")) {
			continue
		}
		header := indentOf(bodies[i])
		kinds[i] = kindExcluded
		entry := -1
		for j := i + 1; j < len(kinds); j++ {
			if kinds[j] != kindProse || listHeader.MatchString(strings.TrimLeft(bodies[j], " ")) {
				break
			}
			indent := indentOf(bodies[j])
			if indent <= header {
				break
			}
			if entry < 0 {
				entry = indent
			}
			if indent == entry {
				kinds[j] = kindExcluded
			}
			i = j
		}
	}
}
