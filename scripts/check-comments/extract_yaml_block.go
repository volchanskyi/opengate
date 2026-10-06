package main

import (
	"regexp"
	"strings"
)

type blockScalar struct {
	start int
	col   int
	shell bool
}

var (
	blockIndicator = regexp.MustCompile(`(^|[\s:-])[|>][+-]?[0-9]?[+-]?$`)
	githubExpr     = regexp.MustCompile(`\$\{\{.*?\}\}`)
)

func (s *yamlScanner) openBlock(i int, code string) *blockScalar {
	trimmed := strings.TrimRight(code, " \t")
	if s.quote != 0 || !blockIndicator.MatchString(trimmed) {
		return nil
	}
	head := strings.TrimRight(trimmed[:strings.LastIndexAny(trimmed, "|>")], " \t")
	if !strings.HasSuffix(head, ":") && !strings.HasSuffix(head, "-") {
		return nil
	}
	col := indentOf(trimmed)
	key := strings.TrimLeft(head, " \t")
	for strings.HasPrefix(key, "- ") {
		key = strings.TrimLeft(key[2:], " ")
		col = strings.Index(trimmed, key)
	}
	if strings.HasSuffix(head, "-") {
		col = strings.LastIndex(head, "-")
	}
	name := strings.TrimSpace(strings.TrimSuffix(key, ":"))
	return &blockScalar{start: i + 1, col: col, shell: s.lang == langWorkflow && name == "run"}
}

func (s *yamlScanner) closeBlock(block *blockScalar, end int) error {
	last := end - 1
	for last >= block.start && strings.TrimSpace(s.lines[last]) == "" {
		last--
	}
	if last < block.start {
		return nil
	}
	if block.shell {
		return s.shellBlock(block.start, last)
	}
	for k := block.start; k <= last; k++ {
		s.dataLines[k+1] = true
	}
	return nil
}

func (s *yamlScanner) shellBlock(first, last int) error {
	indent := -1
	for k := first; k <= last; k++ {
		line := strings.TrimRight(s.lines[k], "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if indent < 0 || indentOf(line) < indent {
			indent = indentOf(line)
		}
	}
	var script strings.Builder
	for k := first; k <= last; k++ {
		line := strings.TrimRight(s.lines[k], "\r")
		if len(line) >= indent {
			line = line[indent:]
		} else {
			line = ""
		}
		script.WriteString(githubExpr.ReplaceAllStringFunc(line, func(expr string) string {
			return strings.Repeat("x", len(expr))
		}))
		script.WriteString("\n")
	}
	tree, err := s.env.shfmtTree("run.bash", []byte(script.String()))
	if err != nil {
		return err
	}
	var found []shellComment
	collectShellComments(tree, &found)
	s.comments = append(s.comments, shellComments(s.content, found, first, indent)...)
	return nil
}
