package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/volchanskyi/opengate/server/internal/relay"
)

// discardLogger is a no-op logger for option-assembly tests.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// TestParsePositiveDuration covers the optional duration overrides
// (OPENGATE_DEGRADED_THRESHOLD / OPENGATE_AFFINITY_TTL) that let the relay's 30s
// degraded-mode and affinity-reclaim timers be shortened. Only a parseable,
// strictly-positive duration overrides; anything else falls back to the relay
// default (ok=false).
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

// TestBuildRelayOptions verifies the degraded-threshold override is appended
// only when OPENGATE_DEGRADED_THRESHOLD holds a valid positive duration, leaving
// the lone base registry option otherwise untouched.
func TestBuildRelayOptions(t *testing.T) {
	reg := relay.NewInProcessRegistry()
	logger := discardLogger()

	t.Run("no_override_keeps_base_option", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "")
		if got := len(buildRelayOptions(reg, logger)); got != 1 {
			t.Fatalf("buildRelayOptions without override = %d options, want 1", got)
		}
	})

	t.Run("degraded_override_appends_option", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "3s")
		if got := len(buildRelayOptions(reg, logger)); got != 2 {
			t.Fatalf("buildRelayOptions with degraded override = %d options, want 2", got)
		}
	})

	t.Run("invalid_override_ignored", func(t *testing.T) {
		t.Setenv("OPENGATE_DEGRADED_THRESHOLD", "nonsense")
		if got := len(buildRelayOptions(reg, logger)); got != 1 {
			t.Fatalf("buildRelayOptions with invalid override = %d options, want 1", got)
		}
	})
}
