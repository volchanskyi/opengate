package main

import "testing"

// TestPortOf covers extracting the port from a listen address, which the
// HTTPPeerDialer reuses to reach homogeneous peers on the same internal port.
func TestPortOf(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{"host_and_port", "10.0.0.5:9091", "9091"},
		{"colon_port_only", ":9091", "9091"},
		{"bare_port_no_colon", "9091", "9091"},
		{"ipv6_host_and_port", "[::1]:9091", "9091"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := portOf(tt.addr); got != tt.want {
				t.Fatalf("portOf(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}
