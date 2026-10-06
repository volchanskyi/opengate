package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func git(root string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", root}, args...)...).Output()
}

func uniqueNames(outputs ...[]byte) []string {
	seen := map[string]bool{}
	var names []string
	for _, output := range outputs {
		for _, name := range strings.Split(string(output), "\x00") {
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func listFiles(root string, paths []string) ([]string, error) {
	output, err := git(root, append([]string{"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, fmt.Errorf("git ls-files in %s: %w", root, err)
	}
	var files []string
	for _, name := range uniqueNames(output) {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			files = append(files, name)
		}
	}
	return files, nil
}

func changedFiles(root, base string, paths []string) ([]string, error) {
	diff, err := git(root, append([]string{"diff", "--name-only", "-z", base, "--"}, paths...)...)
	if err != nil {
		return nil, fmt.Errorf("git diff against %s: %w", base, err)
	}
	untracked, err := git(root, append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	return uniqueNames(diff, untracked), nil
}

func gitShow(root, base, rel string) ([]byte, bool) {
	content, err := git(root, "show", base+":"+rel)
	if err != nil {
		return nil, false
	}
	return content, true
}
