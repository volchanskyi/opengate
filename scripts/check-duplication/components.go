package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	componentRef      = 1
	componentType     = 4
	componentIsTest   = 5
	componentChildren = 7
	componentLines    = 11
	componentPath     = 14
	typeFile          = 4
)

func (r *report) walk(dir string, ref int, seen map[int]bool) error {
	if seen[ref] {
		return fmt.Errorf("component %d appears twice in the tree", ref)
	}
	seen[ref] = true
	data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("component-%d.pb", ref)))
	if err != nil {
		return fmt.Errorf("component %d: %w", ref, err)
	}
	fields, err := decodeMessage(data)
	if err != nil {
		return fmt.Errorf("component %d: %w", ref, err)
	}
	record, children, kind, err := readComponent(ref, fields)
	if err != nil {
		return err
	}
	if kind == typeFile {
		r.files = append(r.files, record)
		if err := r.readDuplications(dir, record); err != nil {
			return err
		}
	}
	for _, child := range children {
		if err := r.walk(dir, child, seen); err != nil {
			return err
		}
	}
	return nil
}

func readComponent(ref int, fields []field) (fileRecord, []int, int, error) {
	record := fileRecord{ref: -1}
	var children []int
	kind, hasPath, hasLines := 0, false, false
	name := fmt.Sprintf("component %d", ref)
	for _, item := range fields {
		var err error
		var test int
		switch item.number {
		case componentRef:
			record.ref, err = varintField(item, name)
		case componentType:
			kind, err = varintField(item, name)
		case componentIsTest:
			test, err = varintField(item, name)
			record.test = test != 0
		case componentLines:
			record.lines, err = varintField(item, name)
			hasLines = true
		case componentPath:
			err = expectWire(item, wireBytes, name)
			record.path, hasPath = string(item.data), true
		case componentChildren:
			children, err = childRefs(item, children)
		}
		if err != nil {
			return fileRecord{}, nil, 0, err
		}
	}
	if record.ref != ref {
		return fileRecord{}, nil, 0, fmt.Errorf("%s records ref %d", name, record.ref)
	}
	if kind == typeFile && (!hasPath || !hasLines) {
		return fileRecord{}, nil, 0, fmt.Errorf("%s is a file with no path or no line count", name)
	}
	return record, children, kind, nil
}

func childRefs(item field, children []int) ([]int, error) {
	if item.wire == wireVarint {
		return append(children, int(item.value)), nil
	}
	if item.wire != wireBytes {
		return nil, fmt.Errorf("child references have wire type %d", item.wire)
	}
	values, err := packedVarints(item.data)
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		children = append(children, int(value))
	}
	return children, nil
}
