package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepeatedShareIsTheUnionOfRepeatedLinesOverTheFile(t *testing.T) {
	dir := writeReport(t,
		fakeFile{ref: 2, path: "a.go", lines: 100, dups: delimited(
			duplicationRecord(1, 10, 3, 1, 10),
			duplicationRecord(5, 12, 3, 20, 27),
			duplicationRecord(40, 41, 0, 60, 61),
		)},
		fakeFile{ref: 3, path: "b.go", lines: 200},
	)
	shares := mustRead(t, dir).shares()
	if got := shares["a.go"]; got.repeated != 16 || got.lines != 100 {
		t.Fatalf("a.go repeats 1-12, 40-41 and 60-61: got %+v", got)
	}
	if got := shares["b.go"]; got.repeated != 0 {
		t.Fatalf("b.go has no records of its own: got %+v", got)
	}
}

func TestOnlyProductionFilesAboveTheCeilingAreRefused(t *testing.T) {
	dir := writeReport(t,
		fakeFile{ref: 2, path: "at.go", lines: 100, dups: delimited(duplicationRecord(1, 3, 9, 1, 3))},
		fakeFile{ref: 3, path: "over.go", lines: 100, dups: delimited(duplicationRecord(1, 4, 9, 1, 4))},
		fakeFile{ref: 4, path: "over_test.go", lines: 10, test: true, dups: delimited(duplicationRecord(1, 9, 9, 1, 9))},
		fakeFile{ref: 9, path: "other.go", lines: 1000},
	)
	above, production := mustRead(t, dir).above(3)
	if production != 3 {
		t.Fatalf("three production files, got %d", production)
	}
	if len(above) != 1 || above[0].path != "over.go" {
		t.Fatalf("only over.go exceeds 3%%, got %+v", above)
	}
}

func TestTheReportIsRefusedWhenItCannotBeRead(t *testing.T) {
	cases := map[string]func(dir string) error{
		"no metadata":   func(dir string) error { return os.Remove(filepath.Join(dir, "metadata.pb")) },
		"no root":       func(dir string) error { return overwrite(dir, "metadata.pb", bytesField(3, []byte("p"))) },
		"missing child": func(dir string) error { return os.Remove(filepath.Join(dir, "component-2.pb")) },
		"file without a path": func(dir string) error {
			return overwrite(dir, "component-2.pb", join(intField(1, 2), intField(4, 4), intField(11, 5)))
		},
		"path with the wrong wire type": func(dir string) error {
			return overwrite(dir, "component-2.pb", join(intField(1, 2), intField(4, 4), intField(11, 5), intField(14, 7)))
		},
		"truncated duplications": func(dir string) error {
			return overwrite(dir, "duplications-2.pb", []byte{0x20, 0x0a})
		},
	}
	for name, breakIt := range cases {
		dir := writeReport(t, fakeFile{ref: 2, path: "a.go", lines: 10, dups: delimited(duplicationRecord(1, 2, 0, 5, 6))})
		if err := breakIt(dir); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := readReport(dir); err == nil {
			t.Errorf("%s: the report is refused", name)
		}
	}
}

func TestAReportWithNoProductionFilesIsRefused(t *testing.T) {
	loaded := mustRead(t, writeReport(t, fakeFile{ref: 2, path: "a_test.go", lines: 10, test: true}))
	if _, production := loaded.above(3); production != 0 {
		t.Fatalf("no production files, got %d", production)
	}
	var out strings.Builder
	if status := evaluate(loaded, 3, &out); status != 1 || !strings.Contains(out.String(), "read no production files") {
		t.Fatalf("status %d: %s", status, out.String())
	}
}
