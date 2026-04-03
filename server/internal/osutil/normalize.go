// Package osutil provides OS and architecture normalization helpers
// shared between the API and agent-API packages.
package osutil

import "strings"

// NormalizeOS maps an agent's OS pretty name to a GOOS-style value for
// matching against manifests.  E.g. "Ubuntu 22.04 LTS" → "linux".
func NormalizeOS(agentOS string) string {
	lower := strings.ToLower(agentOS)
	switch {
	case lower == "linux" || lower == "windows" || lower == "darwin":
		return lower
	case strings.Contains(lower, "linux"),
		strings.Contains(lower, "ubuntu"),
		strings.Contains(lower, "debian"),
		strings.Contains(lower, "fedora"),
		strings.Contains(lower, "centos"),
		strings.Contains(lower, "rhel"),
		strings.Contains(lower, "arch"),
		strings.Contains(lower, "alpine"):
		return "linux"
	case strings.Contains(lower, "windows"):
		return "windows"
	case strings.Contains(lower, "darwin"), strings.Contains(lower, "macos"):
		return "darwin"
	default:
		return lower
	}
}

// NormalizeArch maps Rust std::env::consts::ARCH values to Go/manifest
// conventions.  E.g. "x86_64" → "amd64", "aarch64" → "arm64".
func NormalizeArch(agentArch string) string {
	switch agentArch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return agentArch
	}
}
