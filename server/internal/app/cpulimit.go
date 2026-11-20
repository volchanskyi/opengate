package app

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// CgroupCPUMax is where a cgroup v2 container reads its processor quota and period.
const CgroupCPUMax = "/sys/fs/cgroup/cpu.max"

// ReadProcessorShare reads the processors the container allows from path: its quota over its
// period in microseconds. An absent file and an unlimited quota return zero.
func ReadProcessorShare(path string) (float64, error) {
	raw, err := readScoped(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read container processor limit: %w", err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return 0, fmt.Errorf("container processor limit %q is no quota and period", raw)
	}
	if fields[0] == "max" {
		return 0, nil
	}
	quota, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse container processor quota %q: %w", fields[0], err)
	}
	period, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || period <= 0 {
		return 0, fmt.Errorf("container processor period %q is no length of time", fields[1])
	}
	return quota / period, nil
}
