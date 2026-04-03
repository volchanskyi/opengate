package osutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeOS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"already linux", "linux", "linux"},
		{"already windows", "windows", "windows"},
		{"ubuntu pretty name", "Ubuntu 22.04.4 LTS", "linux"},
		{"debian pretty name", "Debian GNU/Linux 12 (bookworm)", "linux"},
		{"fedora", "Fedora Linux 40 (Server Edition)", "linux"},
		{"centos", "CentOS Stream 9", "linux"},
		{"alpine", "Alpine Linux v3.19", "linux"},
		{"arch linux", "Arch Linux", "linux"},
		{"rhel", "Red Hat Enterprise Linux 9.3 (Plow)", "linux"},
		{"windows pretty", "Windows 11 Pro", "windows"},
		{"darwin", "darwin", "darwin"},
		{"macos", "macOS 14.4", "darwin"},
		{"unknown", "freebsd", "freebsd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeOS(tt.input))
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"x86_64 to amd64", "x86_64", "amd64"},
		{"aarch64 to arm64", "aarch64", "arm64"},
		{"already amd64", "amd64", "amd64"},
		{"already arm64", "arm64", "arm64"},
		{"unknown passthrough", "riscv64", "riscv64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeArch(tt.input))
		})
	}
}
