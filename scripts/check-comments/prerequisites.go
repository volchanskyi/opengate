package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var shfmtPin = regexp.MustCompile(`TOOL_VERSION_SHFMT="([^"]+)"`)

func checkPrerequisites(e *env, langs map[string]bool) error {
	if langs[langTS] {
		if _, err := exec.LookPath("node"); err != nil {
			return prerequisiteError{"node is not on PATH; install the Node the workflows pin"}
		}
		if _, err := os.Stat(filepath.Join(e.typescript, "lib", "typescript.js")); err != nil {
			return prerequisiteError{fmt.Sprintf("the TypeScript compiler is missing at %s; run npm ci in web/", e.typescript)}
		}
	}
	if langs[langShell] || langs[langWorkflow] {
		return checkShfmt(e)
	}
	return nil
}

func checkShfmt(e *env) error {
	manifest, err := os.ReadFile(filepath.Join(e.root, "scripts", "lib", "tool-versions.sh"))
	if err != nil {
		return prerequisiteError{"scripts/lib/tool-versions.sh is missing, so the shfmt pin is unknown"}
	}
	pin := shfmtPin.FindSubmatch(manifest)
	if pin == nil {
		return prerequisiteError{"scripts/lib/tool-versions.sh names no TOOL_VERSION_SHFMT"}
	}
	output, err := exec.Command(e.shfmt, "--version").Output()
	if err != nil {
		return prerequisiteError{fmt.Sprintf("shfmt %s is required; install it with scripts/install-shell-tools.sh", pin[1])}
	}
	got := strings.TrimPrefix(strings.TrimSpace(string(output)), "v")
	if got != string(pin[1]) {
		return prerequisiteError{fmt.Sprintf("shfmt is %s, pinned at %s; install the pin with scripts/install-shell-tools.sh", got, pin[1])}
	}
	return nil
}
