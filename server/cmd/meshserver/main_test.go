package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/volchanskyi/opengate/server/internal/relay"
)

// discardLogger is a no-op logger for option-assembly tests.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

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

// TestParsePositiveDuration covers the optional duration overrides
// (OPENGATE_DEGRADED_THRESHOLD / OPENGATE_AFFINITY_TTL) that let the relay's 30s
// timers be shortened for the multiserver e2e. Only a parseable, strictly-positive
// duration overrides; anything else falls back to the relay default (ok=false).
func TestParsePositiveDuration(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"empty_uses_default", "", 0, false},
		{"valid_seconds", "3s", 3 * time.Second, true},
		{"valid_millis", "500ms", 500 * time.Millisecond, true},
		{"valid_minutes", "2m", 2 * time.Minute, true},
		{"unparseable", "abc", 0, false},
		{"bare_number_no_unit", "5", 0, false},
		{"zero_rejected", "0s", 0, false},
		{"negative_rejected", "-5s", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parsePositiveDuration(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("parsePositiveDuration(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("parsePositiveDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestBuildRelayOptions verifies the degraded-threshold override is appended only
// when OPENGATE_DEGRADED_THRESHOLD holds a valid positive duration, leaving the
// base registry + peer-dialer options otherwise untouched.
func TestBuildRelayOptions(t *testing.T) {
	reg := relay.NewInProcessRegistry()
	dialer := relay.PeerDialer(nil)
	logger := discardLogger()

	t.Run("no_override_keeps_base_options", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "")
		t.Setenv("OPENGATE_AFFINITY_TTL", "")
		if got := len(buildRelayOptions(reg, "srv-a", dialer, logger)); got != 2 {
			t.Fatalf("buildRelayOptions without override = %d options, want 2", got)
		}
	})

	t.Run("degraded_override_appends_option", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "3s")
		t.Setenv("OPENGATE_AFFINITY_TTL", "")
		if got := len(buildRelayOptions(reg, "srv-a", dialer, logger)); got != 3 {
			t.Fatalf("buildRelayOptions with degraded override = %d options, want 3", got)
		}
	})

	t.Run("both_overrides_append_two_options", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "3s")
		t.Setenv("OPENGATE_AFFINITY_TTL", "5s")
		if got := len(buildRelayOptions(reg, "srv-a", dialer, logger)); got != 4 {
			t.Fatalf("buildRelayOptions with both overrides = %d options, want 4", got)
		}
	})

	t.Run("invalid_overrides_ignored", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "nonsense")
		t.Setenv("OPENGATE_AFFINITY_TTL", "-1s")
		if got := len(buildRelayOptions(reg, "srv-a", dialer, logger)); got != 2 {
			t.Fatalf("buildRelayOptions with invalid overrides = %d options, want 2", got)
		}
	})
}
