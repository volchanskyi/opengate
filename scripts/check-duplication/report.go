package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const metadataRoot = 5

type fileRecord struct {
	ref   int
	path  string
	lines int
	test  bool
}

type lineRange struct {
	start int
	end   int
}

type report struct {
	files    []fileRecord
	repeated map[int][]lineRange
}

type share struct {
	path     string
	repeated int
	lines    int
}

func (s share) percent() float64 {
	if s.lines == 0 {
		return 0
	}
	return float64(s.repeated) * 100 / float64(s.lines)
}

func readReport(dir string) (report, error) {
	metadata, err := os.ReadFile(filepath.Join(dir, "metadata.pb"))
	if err != nil {
		return report{}, err
	}
	fields, err := decodeMessage(metadata)
	if err != nil {
		return report{}, fmt.Errorf("metadata: %w", err)
	}
	root := 0
	for _, item := range fields {
		if item.number == metadataRoot {
			if root, err = varintField(item, "metadata"); err != nil {
				return report{}, err
			}
		}
	}
	if root == 0 {
		return report{}, errors.New("metadata names no root component")
	}
	loaded := report{repeated: map[int][]lineRange{}}
	if err := loaded.walk(dir, root, map[int]bool{}); err != nil {
		return report{}, err
	}
	sort.Slice(loaded.files, func(i, j int) bool { return loaded.files[i].path < loaded.files[j].path })
	return loaded, nil
}

func (r report) shares() map[string]share {
	out := map[string]share{}
	for _, file := range r.files {
		lines := map[int]bool{}
		for _, item := range r.repeated[file.ref] {
			for line := item.start; line <= item.end && line <= file.lines; line++ {
				lines[line] = true
			}
		}
		out[file.path] = share{path: file.path, repeated: len(lines), lines: file.lines}
	}
	return out
}

func (r report) above(ceiling float64) ([]share, int) {
	shares := r.shares()
	var over []share
	production := 0
	for _, file := range r.files {
		if file.test {
			continue
		}
		production++
		if item := shares[file.path]; item.percent() > ceiling {
			over = append(over, item)
		}
	}
	return over, production
}
