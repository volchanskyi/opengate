package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVictoriaMetricsReadsStayBehindScopedClient(t *testing.T) {
	t.Parallel()
	internalRoot := filepath.Clean("..")
	err := filepath.WalkDir(internalRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "telemetry" || d.Name() == "testvm" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		srcBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(srcBytes)
		if strings.Contains(src, "/api/v1/export") || strings.Contains(src, "/api/v1/query") {
			t.Errorf("%s queries VictoriaMetrics directly; use telemetry.VMClient so org_id is injected", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
