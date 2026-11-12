package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// CgroupMemoryMax is where a cgroup v2 container reads its memory limit.
const CgroupMemoryMax = "/sys/fs/cgroup/memory.max"

// memoryLimitShare keeps 10% of the container for memory Go does not manage; the collector then
// works to the limit, where without one the heap grows to twice its live size.
const memoryLimitShare = 0.9

// ApplyMemoryLimit sets Go's memory limit to 90% of the container limit read from path and returns
// it. An explicit GOMEMLIMIT, an absent file and an unlimited container set nothing.
func ApplyMemoryLimit(path string) (int64, error) {
	if _, set := os.LookupEnv("GOMEMLIMIT"); set {
		return 0, nil
	}
	raw, err := readScoped(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read container memory limit: %w", err)
	}
	value := strings.TrimSpace(string(raw))
	if value == "max" {
		return 0, nil
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse container memory limit %q: %w", value, err)
	}
	soft := int64(float64(limit) * memoryLimitShare)
	debug.SetMemoryLimit(soft)
	return soft, nil
}

// readScoped reads the file at path through a root opened on its directory, so the read stays
// inside that directory.
func readScoped(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.Base(path))
}
