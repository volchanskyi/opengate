package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func varint(value uint64) []byte {
	var out []byte
	for value >= 0x80 {
		out = append(out, byte(value)|0x80)
		value >>= 7
	}
	return append(out, byte(value))
}

func intField(number int, value uint64) []byte {
	return append(varint(uint64(number<<3)), varint(value)...)
}

func bytesField(number int, payload []byte) []byte {
	out := varint(uint64(number<<3 | 2))
	out = append(out, varint(uint64(len(payload)))...)
	return append(out, payload...)
}

func join(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func delimited(messages ...[]byte) []byte {
	var out []byte
	for _, message := range messages {
		out = append(out, varint(uint64(len(message)))...)
		out = append(out, message...)
	}
	return out
}

func textRange(start, end int) []byte {
	return join(intField(1, uint64(start)), intField(2, uint64(end)))
}

func duplicationRecord(start, end, otherRef, otherStart, otherEnd int) []byte {
	duplicate := bytesField(2, textRange(otherStart, otherEnd))
	if otherRef > 0 {
		duplicate = join(intField(1, uint64(otherRef)), duplicate)
	}
	return join(bytesField(1, textRange(start, end)), bytesField(2, duplicate))
}

type fakeFile struct {
	ref   int
	path  string
	lines int
	test  bool
	dups  []byte
}

func overwrite(dir, name string, content []byte) error {
	return os.WriteFile(filepath.Join(dir, name), content, 0o600)
}

func writeReport(t *testing.T, files ...fakeFile) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, content []byte) {
		if err := overwrite(dir, name, content); err != nil {
			t.Fatal(err)
		}
	}
	write("metadata.pb", join(bytesField(3, []byte("project")), intField(5, 1)))
	var children []byte
	for _, file := range files {
		children = append(children, varint(uint64(file.ref))...)
		record := join(intField(1, uint64(file.ref)), intField(4, 4), intField(11, uint64(file.lines)), bytesField(14, []byte(file.path)))
		if file.test {
			record = join(record, intField(5, 1))
		}
		write(fmt.Sprintf("component-%d.pb", file.ref), record)
		if file.dups != nil {
			write(fmt.Sprintf("duplications-%d.pb", file.ref), file.dups)
		}
	}
	root := join(intField(1, 1), intField(4, 1))
	if len(children) > 0 {
		root = join(root, bytesField(7, children))
	}
	write("component-1.pb", root)
	return dir
}

func mustRead(t *testing.T, dir string) report {
	t.Helper()
	loaded, err := readReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}
