package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func untar(archive io.Reader, dir string) error {
	unzipped, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(header.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o600); err != nil {
			return err
		}
	}
}

func extractFixture(t *testing.T) string {
	t.Helper()
	archive, err := os.Open(filepath.Join("..", "tests", "fixtures", "check-duplication", "real-report.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	dir := t.TempDir()
	if err := untar(archive, dir); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "scanner-report")
}

func TestTheRealReportReadsAsSonarComputedIt(t *testing.T) {
	above, production := mustRead(t, extractFixture(t)).above(3)
	if production < 400 {
		t.Fatalf("the real report holds over 400 production files, read %d", production)
	}
	want := map[string]string{
		"agent/crates/edge-tsdb/src/redb_compact.rs": "20.1",
		"agent/crates/edge-tsdb/src/redb_store.rs":   "22.9",
		"web/src/lib/transport/ws-transport.ts":      "15.7",
		"web/src/lib/transport/webrtc-transport.ts":  "9.4",
	}
	if len(above) != len(want) {
		t.Fatalf("want %d files above 3%%, got %+v", len(want), above)
	}
	for _, item := range above {
		if got := fmt.Sprintf("%.1f", item.percent()); want[item.path] != got {
			t.Errorf("%s: want %s%%, got %s%%", item.path, want[item.path], got)
		}
	}
}
