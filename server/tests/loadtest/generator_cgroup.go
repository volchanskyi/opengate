package main

import (
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	cgroupRoot     = "/sys/fs/cgroup"
	procSelfCgroup = "/proc/self/cgroup"
)

type cgroupCPU struct {
	UsageMicros int64
	// RefusedMicros is throttled time; Refusals reports whether the kernel keeps that account.
	RefusedMicros int64
	Refusals      bool
}

// cgroupFiles scopes every read to the mount, so a path taken from /proc/self/cgroup
// cannot reach outside the hierarchy.
type cgroupFiles struct {
	root *os.Root
	dir  string
}

func openCgroupFiles(mount, dir string) (cgroupFiles, bool) {
	root, err := os.OpenRoot(mount)
	if err != nil {
		return cgroupFiles{}, false
	}
	files := cgroupFiles{root: root, dir: dir}
	if _, err := files.read("cpu.max"); err != nil {
		files.close()
		return cgroupFiles{}, false
	}
	return files, true
}

func (c cgroupFiles) read(name string) ([]byte, error) {
	return c.root.ReadFile(path.Join(c.dir, name))
}

func (c cgroupFiles) close() {
	if c.root != nil {
		_ = c.root.Close()
	}
}

// readGeneratorAllowance reports no allowance for a cgroup without a processor quota.
func readGeneratorAllowance(files cgroupFiles) (GeneratorAllowance, bool) {
	raw, err := files.read("cpu.max")
	if err != nil {
		return GeneratorAllowance{}, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 || fields[0] == "max" {
		return GeneratorAllowance{}, false
	}
	quota, quotaErr := strconv.ParseFloat(fields[0], 64)
	period, periodErr := strconv.ParseFloat(fields[1], 64)
	if quotaErr != nil || periodErr != nil || period <= 0 || quota <= 0 {
		return GeneratorAllowance{}, false
	}

	allowance := GeneratorAllowance{Processors: quota / period}
	// A memory ceiling of "max" leaves MemoryBytes zero.
	if limit := readInt64Account(files, "memory.max"); limit > 0 {
		allowance.MemoryBytes = limit
	}
	return allowance, true
}

func readCgroupCPU(files cgroupFiles) (cgroupCPU, bool) {
	raw, err := files.read("cpu.stat")
	if err != nil {
		return cgroupCPU{}, false
	}
	var cpu cgroupCPU
	var sawUsage bool
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "usage_usec":
			cpu.UsageMicros = value
			sawUsage = true
		case "throttled_usec":
			cpu.RefusedMicros = value
			cpu.Refusals = true
		}
	}
	return cpu, sawUsage
}

// openOwnCgroup opens the cgroup holding the process's processor allowance: its own, or the
// nearest one above it, such as a slice the generators share.
func openOwnCgroup() (cgroupFiles, bool) {
	return allowanceCgroup(cgroupRoot, ownCgroupCandidates())
}

// allowanceCgroup walks up from each candidate under mount to the nearest cgroup with a
// processor quota. A candidate of "." is the process's cgroup inside a cgroup namespace.
func allowanceCgroup(mount string, candidates []string) (cgroupFiles, bool) {
	for _, own := range candidates {
		for dir := own; ; dir = path.Dir(dir) {
			if files, ok := openCgroupFiles(mount, dir); ok {
				if _, granted := readGeneratorAllowance(files); granted {
					return files, true
				}
				files.close()
			}
			if dir == "." || dir == "/" {
				break
			}
		}
	}
	return cgroupFiles{}, false
}

func ownCgroupCandidates() []string {
	candidates := []string{"."}
	raw, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return candidates
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		// The unified hierarchy's line is "0::<path>".
		own, found := strings.CutPrefix(strings.TrimSpace(line), "0::")
		if !found {
			continue
		}
		if own = strings.TrimPrefix(own, "/"); own != "" {
			candidates = append([]string{own}, candidates...)
		}
	}
	return candidates
}

// readInt64Account returns zero where the account is absent or holds a non-number such as "max".
func readInt64Account(files cgroupFiles, name string) int64 {
	raw, err := files.read(name)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
