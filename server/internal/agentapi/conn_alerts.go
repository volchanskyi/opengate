package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Alert admission. An alert is the only thing that carries the detail behind a
// signal: the server keeps no high-resolution history to go back to and never
// asks the device for more. So every alert refused here is an incident that
// cannot be reconstructed afterwards, and every refusal is counted under its own
// reason rather than dropped quietly.
//
// What the endpoint sends is untrusted input, so the shape is checked before
// anything is believed: a bounded payload, a severity from the closed set, and a
// complete identity.

const (
	// alertEnvelopeHeadroomBytes is what the alert's own fields are allowed to
	// weigh around its evidence — ids, a rule name, a metric label, four
	// timestamps, and the msgpack keys naming them. Eight kilobytes is far more
	// than that costs, which is the point: the bound must never be what refuses
	// an alert carrying the largest evidence the contract allows.
	alertEnvelopeHeadroomBytes = 8 * 1024

	// maxAlertPayloadBytes is the alert path's own bound, sized from the evidence
	// cap rather than borrowed from the telemetry path. The two happen to name
	// the same number today (64 KiB), so an alert measured against the telemetry
	// bound would be refused for carrying exactly the evidence it is supposed to
	// carry — and "truncate, never reject" would be quietly defeated by a bound
	// nobody had looked at. Different paths, different risk, different budget.
	maxAlertPayloadBytes = protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes

	// Why an alert was refused. Each cause is its own label, so a fleet-wide
	// rollout bug and one misbehaving device never look like the same number.
	alertDropPayloadTooLarge      = "alert_payload_too_large"
	alertDropSeverityUnknown      = "alert_severity_unknown"
	alertDropIdentityIncomplete   = "alert_identity_incomplete"
	alertDropEvidenceCodecUnknown = "alert_evidence_codec_unknown"
)

// handleAgentAlert admits one alert from the device.
//
// It returns nil for a refused alert as well as an admitted one: a malformed
// alert is a fact about that message, not a reason to tear down a control
// channel that also carries this device's remote-management paths.
func (a *AgentConn) handleAgentAlert(_ context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if payloadLen > maxAlertPayloadBytes {
		a.dropTelemetry(alertDropPayloadTooLarge, "bytes", payloadLen, "max", maxAlertPayloadBytes)
		return nil
	}
	// Severity is always stated on the wire, so an absent one is a broken sender
	// rather than a quiet device. Reading it as Info would turn a critical alert
	// into a line nobody looks at.
	if msg.Severity == nil || !protocol.ValidAlertSeverity(*msg.Severity) {
		a.dropTelemetry(alertDropSeverityUnknown, "severity", severityLabel(msg.Severity))
		return nil
	}
	if !hasAlertIdentity(msg) {
		a.dropTelemetry(alertDropIdentityIncomplete,
			"rule_id", msg.RuleID, "rule_version", msg.RuleVersion,
			"window_start_ts", msg.WindowStartTS, "window_end_ts", msg.WindowEndTS)
		return nil
	}
	// Evidence is optional — a device that had nothing to attach still says the
	// machine is in trouble. Evidence under a codec this build cannot read is
	// not: storing an unreadable blob beside an alert is worse than storing
	// none, because it reads as evidence that exists.
	if len(msg.Evidence) > 0 && msg.EvidenceCodec != protocol.EvidenceCodec {
		a.dropTelemetry(alertDropEvidenceCodecUnknown, "codec", msg.EvidenceCodec)
		return nil
	}

	// Admitted. The alert is not counted against the telemetry ingest ledger,
	// because that ledger's invariant is that everything counted as ingested
	// either produced state or was filed under a drop reason — and the alert
	// store is not built yet. Counting an admission that persists nothing would
	// put a number in the ledger with nothing on the other side of it, which is
	// the failure the ledger exists to catch.
	a.logger.Debug("admitted device alert",
		"device_id", a.DeviceID, "rule_id", msg.RuleID, "rule_version", msg.RuleVersion,
		"severity", string(*msg.Severity), "backfilled", msg.Backfilled != nil && *msg.Backfilled,
		"evidence_bytes", len(msg.Evidence))
	return nil
}

// hasAlertIdentity reports whether the alert carries the parts that identify it
// independently of the id the device chose.
//
// (device, rule, version, window start) is what lets a reconnect replay resolve
// to the row it already wrote. An alert missing any part of it cannot be
// deduplicated, so it is refused rather than stored under a null that would
// duplicate itself on the next reconnect. The window is required to run forwards
// for the same reason: a window whose end precedes its start describes no
// interval, and every later read of it would have to invent one.
func hasAlertIdentity(msg *protocol.ControlMessage) bool {
	return msg.RuleID != "" &&
		msg.RuleVersion != 0 &&
		msg.WindowStartTS > 0 &&
		msg.WindowEndTS >= msg.WindowStartTS
}

// severityLabel renders a severity for a log line, naming the absent case rather
// than logging an empty string that reads like a value.
func severityLabel(severity *protocol.AlertSeverity) string {
	if severity == nil {
		return "(absent)"
	}
	return string(*severity)
}
