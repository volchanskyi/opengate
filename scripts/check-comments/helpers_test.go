package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func testEnv(t *testing.T) *env {
	t.Helper()
	typescript, err := filepath.Abs(filepath.Join("..", "..", "web", "node_modules", "typescript"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{typescript: typescript, shfmt: "shfmt"}
}

func extractOne(t *testing.T, rel, content string) fileResult {
	t.Helper()
	results, err := testEnv(t).extractAll([]source{{rel: rel, content: []byte(content)}}, true)
	if err != nil {
		t.Fatalf("extract %s: %v", rel, err)
	}
	if len(results) != 1 {
		t.Fatalf("extract %s: got %d results", rel, len(results))
	}
	if results[0].err != nil {
		t.Fatalf("extract %s: %v", rel, results[0].err)
	}
	return results[0]
}

func bodiesOf(t *testing.T, rel, content string) []string {
	t.Helper()
	result := extractOne(t, rel, content)
	var bodies []string
	for _, line := range commentLines(result) {
		bodies = append(bodies, fmt.Sprintf("%d:%t:%s", line.Line, line.OwnLine, line.Body))
	}
	return bodies
}

func violationsOf(t *testing.T, rel, content string) []string {
	t.Helper()
	result := extractOne(t, rel, content)
	var found []string
	for _, item := range analyze(result) {
		found = append(found, fmt.Sprintf("%d:%s", item.Line, item.Code))
	}
	sort.Strings(found)
	return found
}

func expectEqual(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got  %q\n want %q", name, got, want)
	}
}

func sorted(values ...string) []string {
	sort.Strings(values)
	return values
}
