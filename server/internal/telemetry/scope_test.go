package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVictoriaMetricsReadsStayBehindScopedClient(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir(filepath.Clean(".."), func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return skipTelemetryScanDirs(d)
		}
		return assertNoDirectVMQuery(t, path)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// skipTelemetryScanDirs prunes packages that legitimately hold the scoped VM
// client itself (telemetry) or the test harness (testvm).
func skipTelemetryScanDirs(d os.DirEntry) error {
	if d.Name() == "telemetry" || d.Name() == "testvm" {
		return filepath.SkipDir
	}
	return nil
}

// assertNoDirectVMQuery fails if a production Go file reaches VictoriaMetrics'
// export/query endpoints directly instead of through telemetry.VMClient.
func assertNoDirectVMQuery(t *testing.T, path string) error {
	t.Helper()
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
}
