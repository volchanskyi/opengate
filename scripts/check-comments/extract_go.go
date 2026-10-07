package main

import (
	"bytes"
	"go/scanner"
	"go/token"
	"strings"
)

func extractGo(content []byte) ([]comment, []string) {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", fileSet.Base(), len(content))
	var scan scanner.Scanner
	scan.Init(file, content, func(token.Position, string) {}, scanner.ScanComments)
	var comments []comment
	var units []string
	for {
		position, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		offset := file.Offset(position)
		if kind == token.COMMENT {
			comments = append(comments, goComment(content, offset, literal))
			continue
		}
		units = append(units, kind.String()+" "+literal)
	}
	return comments, units
}

func goComment(content []byte, offset int, literal string) comment {
	if strings.HasPrefix(literal, "/*") {
		end := bytes.Index(content[offset+2:], []byte("*/"))
		if end < 0 {
			return comment{start: offset, end: len(content), open: "/*", close: "*/"}
		}
		return comment{start: offset, end: offset + 2 + end + 2, open: "/*", close: "*/"}
	}
	end := bytes.IndexByte(content[offset:], '\n')
	if end < 0 {
		return comment{start: offset, end: len(content), open: "//"}
	}
	return comment{start: offset, end: offset + end, open: "//"}
}
