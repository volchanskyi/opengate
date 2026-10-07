package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	duplicationOrigin = 1
	duplicationOther  = 2
	duplicateFileRef  = 1
	duplicateRange    = 2
	rangeStart        = 1
	rangeEnd          = 2
)

func (r *report) readDuplications(dir string, file fileRecord) error {
	data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("duplications-%d.pb", file.ref)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	messages, err := decodeDelimited(data)
	if err != nil {
		return fmt.Errorf("duplications of %s: %w", file.path, err)
	}
	for _, message := range messages {
		ranges, err := duplicationRanges(file.ref, message)
		if err != nil {
			return fmt.Errorf("duplications of %s: %w", file.path, err)
		}
		r.repeated[file.ref] = append(r.repeated[file.ref], ranges...)
	}
	return nil
}

func duplicationRanges(ref int, message []byte) ([]lineRange, error) {
	fields, err := decodeMessage(message)
	if err != nil {
		return nil, err
	}
	var ranges []lineRange
	hasOrigin := false
	for _, item := range fields {
		switch item.number {
		case duplicationOrigin:
			origin, err := readRange(item)
			if err != nil {
				return nil, err
			}
			ranges, hasOrigin = append(ranges, origin), true
		case duplicationOther:
			same, other, err := readDuplicate(ref, item)
			if err != nil {
				return nil, err
			}
			if same {
				ranges = append(ranges, other)
			}
		}
	}
	if !hasOrigin {
		return nil, errors.New("a duplication record has no origin range")
	}
	return ranges, nil
}

func readDuplicate(ref int, item field) (bool, lineRange, error) {
	if err := expectWire(item, wireBytes, "duplicate"); err != nil {
		return false, lineRange{}, err
	}
	fields, err := decodeMessage(item.data)
	if err != nil {
		return false, lineRange{}, err
	}
	other, found := 0, lineRange{}
	hasRange := false
	for _, inner := range fields {
		switch inner.number {
		case duplicateFileRef:
			other, err = varintField(inner, "duplicate")
		case duplicateRange:
			found, err = readRange(inner)
			hasRange = true
		}
		if err != nil {
			return false, lineRange{}, err
		}
	}
	if !hasRange {
		return false, lineRange{}, errors.New("a duplicate has no range")
	}
	return other == 0 || other == ref, found, nil
}

func readRange(item field) (lineRange, error) {
	if err := expectWire(item, wireBytes, "text range"); err != nil {
		return lineRange{}, err
	}
	fields, err := decodeMessage(item.data)
	if err != nil {
		return lineRange{}, err
	}
	var found lineRange
	for _, inner := range fields {
		switch inner.number {
		case rangeStart:
			found.start, err = varintField(inner, "text range")
		case rangeEnd:
			found.end, err = varintField(inner, "text range")
		}
		if err != nil {
			return lineRange{}, err
		}
	}
	if found.start < 1 || found.end < found.start {
		return lineRange{}, fmt.Errorf("text range %d-%d is not a line range", found.start, found.end)
	}
	return found, nil
}
