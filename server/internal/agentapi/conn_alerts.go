package agentapi

import (
	"bytes"
	"compress/flate"
	"context"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Alert admission. An alert is the only thing that carries the detail behind a
// signal: the server keeps no high-resolution history to go back to and never
// asks the device for more. So every alert refused here is an incident that
// cannot be reconstructed afterwards, and every refusal is counted under its own
// reason rather than dropped quietly.
//
// What the endpoint sends is untrusted input, so the shape is checked before
// anything is believed: a bounded payload, a severity from the closed set, a
// complete identity, a rule this build actually ships, timestamps inside the
// window its own kind of alert is allowed, and evidence that reads back.

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

	// maxEvidenceInflatedBytes bounds what compressed evidence is allowed to
	// expand to while it is being checked. The composition is fixed — eight
	// ranked dimensions, three series of at most 512 points, ten processes,
	// twenty log lines — so a megabyte is orders of magnitude more than any
	// honest blob needs. What it refuses is the dishonest one: 64 KiB of
	// DEFLATE can name gigabytes of output, and inflating it to find out would
	// be the server doing the endpoint's bidding.
	maxEvidenceInflatedBytes = 1 << 20

	// fallbackGroupWindow is how long two firings stay one room when the rule
	// that raised them cannot be resolved. A quarter of an hour is the shortest
	// hold any shipped rule declares, so it can only ever under-group.
	fallbackGroupWindow = 15 * time.Minute

	// Why an alert was refused. Each cause is its own label, so a fleet-wide
	// rollout bug and one misbehaving device never look like the same number.
	alertDropPayloadTooLarge      = "alert_payload_too_large"
	alertDropSeverityUnknown      = "alert_severity_unknown"
	alertDropIdentityIncomplete   = "alert_identity_incomplete"
	alertDropRuleUnknown          = "alert_rule_unknown"
	alertDropTimestampOutOfRange  = "alert_timestamp_out_of_range"
	alertDropEvidenceCodecUnknown = "alert_evidence_codec_unknown"
	alertDropEvidenceUndecodable  = "alert_evidence_undecodable"
	alertDropOrganizationUnknown  = "alert_organization_unknown"
	alertDropOrganizationCeiling  = "alert_organization_ceiling"
	alertDropDuplicate            = "alert_duplicate"
)

// AlertRecorder files one alert into the room its rule groups it into, and
// reports what became of it. Three outcomes rather than an error and a success:
// a reconnect replay and a customer's spent budget are both ordinary, and only
// telling them apart lets each be counted under the reason it deserves.
type AlertRecorder interface {
	Record(ctx context.Context, alert alerts.Alert, grouping alerts.Grouping) (alerts.Outcome, error)
}

// handleAgentAlert admits one alert from the device.
//
// It returns nil for a refused alert as well as an admitted one: a malformed
// alert is a fact about that message, not a reason to tear down a control
// channel that also carries this device's remote-management paths.
func (a *AgentConn) handleAgentAlert(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if payloadLen > maxAlertPayloadBytes {
		a.dropTelemetry(alertDropPayloadTooLarge, "bytes", payloadLen, "max", maxAlertPayloadBytes)
		return nil
	}
	// Counted as ingested from here on. Everything below either produces a
	// stored alert or files exactly one typed drop, which is the whole of the
	// ledger's invariant.
	a.acceptedTelemetry(protocol.MsgAgentAlert)

	alert, ok := a.validatedAlert(msg)
	if !ok {
		return nil
	}

	// Alerts arrive on the read loop like every other control message, so the
	// write goes to a bounded slot. A synchronous store write here would stall
	// the channel that also carries this device's remote-management paths — and
	// a storm is exactly when that channel matters most.
	a.persistTelemetry(ctx, 1, func(jobCtx context.Context, _ dbtx.Tenant) error {
		return a.storeAlert(jobCtx, alert)
	})
	return nil
}

// validatedAlert turns a control message into the alert that would be stored,
// or refuses it under exactly one typed reason. The customer it belongs to is
// filled in later, on the slot goroutine, because resolving it reads the
// database and the read loop must not wait for that.
func (a *AgentConn) validatedAlert(msg *protocol.ControlMessage) (alerts.Alert, bool) {
	// Severity is always stated on the wire, so an absent one is a broken sender
	// rather than a quiet device. Reading it as Info would turn a critical alert
	// into a line nobody looks at.
	severity, ok := storedSeverity(msg.Severity)
	if !ok {
		a.dropTelemetry(alertDropSeverityUnknown, "severity", severityLabel(msg.Severity))
		return alerts.Alert{}, false
	}
	if !hasAlertIdentity(msg) {
		a.dropTelemetry(alertDropIdentityIncomplete,
			"rule_id", msg.RuleID, "rule_version", msg.RuleVersion,
			"window_start_ts", msg.WindowStartTS, "window_end_ts", msg.WindowEndTS)
		return alerts.Alert{}, false
	}
	// A rule this build has no definition for cannot be rendered, grouped or
	// retuned by anything downstream. Stored, it would be a row a technician
	// can see and nobody can act on.
	if !a.shipsRule(msg.RuleID) {
		a.dropTelemetry(alertDropRuleUnknown, "rule_id", msg.RuleID)
		return alerts.Alert{}, false
	}
	if !alertTimestampsInRange(msg, time.Now().UTC()) {
		a.dropTelemetry(alertDropTimestampOutOfRange,
			"window_start_ts", msg.WindowStartTS, "observed_ts", msg.ObservedTS,
			"backfilled", isBackfilled(msg))
		return alerts.Alert{}, false
	}
	// Evidence is optional — a device that had nothing to attach still says the
	// machine is in trouble. Evidence under a codec this build cannot read is
	// not: storing an unreadable blob beside an alert is worse than storing
	// none, because it reads as evidence that exists.
	if len(msg.Evidence) > 0 {
		if msg.EvidenceCodec != protocol.EvidenceCodec {
			a.dropTelemetry(alertDropEvidenceCodecUnknown, "codec", msg.EvidenceCodec)
			return alerts.Alert{}, false
		}
		if err := checkEvidenceReadable(msg.Evidence); err != nil {
			a.dropTelemetry(alertDropEvidenceUndecodable, "bytes", len(msg.Evidence), "error", err)
			return alerts.Alert{}, false
		}
	}

	return alerts.Alert{
		// The device's own id, kept so an operator can line a row up against
		// the agent log that produced it. It is not the alert's identity, so an
		// unreadable one costs nothing and a server-side id takes its place.
		ID:            alertRowID(msg.AlertID),
		DeviceID:      a.DeviceID,
		RuleID:        msg.RuleID,
		RuleVersion:   msg.RuleVersion,
		Severity:      severity,
		Metric:        msg.Metric,
		Value:         msg.Value,
		WindowStart:   time.Unix(msg.WindowStartTS, 0).UTC(),
		WindowEnd:     time.Unix(msg.WindowEndTS, 0).UTC(),
		ObservedAt:    time.Unix(msg.ObservedTS, 0).UTC(),
		Backfilled:    isBackfilled(msg),
		Evidence:      msg.Evidence,
		EvidenceCodec: msg.EvidenceCodec,
	}, true
}

// storeAlert resolves the customer the machine belongs to and files the alert,
// counting whatever became of it. Returning an error hands the accounting back
// to the persist path, which counts the message as lost — which is exactly what
// it is, and what makes the endpoint's retry on the next reconnect safe.
func (a *AgentConn) storeAlert(ctx context.Context, alert alerts.Alert) error {
	// Nowhere to put the alert is the same fact as a store that refused it, and
	// it is counted the same way: a deployment missing its store must not read
	// as a fleet whose alerts are all landing.
	if a.alertStore == nil {
		return errNoAlertStore
	}
	// Every scoping key an incident is built on is the customer's, so an alert
	// filed under a guess would land in another customer's room.
	alert.OrganizationID = a.settingsScope(ctx).OrganizationID
	if alert.OrganizationID == uuid.Nil {
		a.dropTelemetry(alertDropOrganizationUnknown, "device_id", a.DeviceID)
		return nil
	}

	outcome, err := a.alertStore.Record(ctx, alert, a.groupingFor(alert.RuleID))
	if err != nil {
		return err
	}
	a.observeAlertOutcome(outcome, alert)
	return nil
}

// groupingFor reads how a rule's alerts fold from the rule's own definition:
// which rung of the tenancy ladder its room is about, and how far apart two
// firings can be and still be the same thing.
//
// A connection wired without a catalogue, or a rule this build has no definition
// for, gets the narrowest room and the shortest hold. Both directions of a guess
// are wrong, but they are not equally wrong: too wide merges two customers'
// unrelated events into one room, and too long holds a room open on a number
// nobody chose. The narrow, short guess can only ever under-group, which shows
// up as more rooms rather than as a wrong one.
func (a *AgentConn) groupingFor(ruleID string) alerts.Grouping {
	def, ok := a.ruleCatalog.Lookup(ruleID)
	if !ok {
		return alerts.Grouping{Scope: alerts.ScopeDevice, Window: fallbackGroupWindow}
	}
	return alerts.Grouping{
		Scope:  incidentScope(def.GroupBy),
		Window: time.Duration(def.GroupWindowSecs) * time.Second,
	}
}

// incidentScope picks how wide a room is from what a rule says its alerts are
// about.
//
// A rule's grouping keys are not all rungs of the tenancy ladder: `mount` and
// `metric` say which volume or dimension a firing was about, which is a property
// of the alert rather than of the room. A machine with a full data volume and a
// full system volume has two problems and two alerts, and one room about that
// machine's disks — the schema has no narrower room to offer, and inventing one
// would give a technician two rooms to work with one machine to visit.
//
// So the room is the narrowest rung the rule actually names, and a rule that
// names none is about the machine that raised it.
func incidentScope(groupBy []string) alerts.Scope {
	scope := alerts.ScopeDevice
	for _, key := range groupBy {
		switch alerts.Scope(key) {
		case alerts.ScopeDevice:
			return alerts.ScopeDevice
		case alerts.ScopeSite:
			scope = alerts.ScopeSite
		case alerts.ScopeOrganization:
			if scope != alerts.ScopeSite {
				scope = alerts.ScopeOrganization
			}
		}
	}
	return scope
}

// observeAlertOutcome counts what became of an alert the store accepted.
//
// A stored alert is new detection and is counted under the rule that raised it —
// that count, divided by the fleet, is the alerts-per-device-per-day figure the
// customer ceiling and the evidence projection are both sized against, so it has
// to be stored rows and nothing else. The other two produced no row, so each
// files a typed drop to keep the ingest ledger balanced — and a spent budget is
// additionally counted as suppression, because unlike a replay it cost the
// customer an incident nobody can reconstruct.
func (a *AgentConn) observeAlertOutcome(outcome alerts.Outcome, alert alerts.Alert) {
	switch outcome {
	case alerts.Stored:
		if a.metrics != nil {
			a.metrics.ObserveAlertCreated(alert.RuleID)
		}
		a.logger.Debug("stored device alert",
			"device_id", a.DeviceID, "organization_id", alert.OrganizationID,
			"rule_id", alert.RuleID, "rule_version", alert.RuleVersion,
			"severity", string(alert.Severity), "backfilled", alert.Backfilled,
			"evidence_bytes", len(alert.Evidence))
	case alerts.Duplicate:
		a.dropTelemetry(alertDropDuplicate, "rule_id", alert.RuleID,
			"window_start", alert.WindowStart)
	case alerts.CeilingSuppressed:
		a.dropTelemetry(alertDropOrganizationCeiling,
			"organization_id", alert.OrganizationID, "rule_id", alert.RuleID)
		if a.metrics != nil {
			a.metrics.ObserveAlertSuppressed(string(alerts.CeilingSuppressed))
		}
	}
}

// shipsRule reports whether this build has a definition for the rule an alert
// names. A connection wired without a catalogue cannot answer, and refusing
// every alert on that basis would silence a fleet over a wiring detail.
func (a *AgentConn) shipsRule(ruleID string) bool {
	if a.ruleCatalog == nil {
		return true
	}
	_, ok := a.ruleCatalog.Lookup(ruleID)
	return ok
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

// alertTimestampsInRange reports whether every timestamp on the alert falls
// inside the window its own kind of alert is allowed.
//
// Nothing is clamped here, and that is deliberate: the window start is part of
// the alert's identity, so pulling it to a bound would make the same alert
// resolve to a different row on every reconnect — the replay would duplicate
// itself instead of deduplicating. A telemetry sample has no identity to lose,
// which is why that path clamps and this one refuses.
//
// A retroactive finding is legitimately old — answering "has this happened
// before?" over months of local history is the whole point of it — so the
// backward bound widens to the same retention the backfill path uses.
func alertTimestampsInRange(msg *protocol.ControlMessage, now time.Time) bool {
	backlog := maxTelemetryBacklog
	if isBackfilled(msg) {
		backlog = backfillRetentionSecs * time.Second
	}
	floor, ceiling := now.Add(-backlog), now.Add(maxTelemetrySkew)
	for _, ts := range []int64{msg.WindowStartTS, msg.WindowEndTS, msg.ObservedTS} {
		if ts <= 0 {
			return false
		}
		at := time.Unix(ts, 0).UTC()
		if at.Before(floor) || at.After(ceiling) {
			return false
		}
	}
	return true
}

// checkEvidenceReadable proves the blob is what its codec says it is, and that
// it does not expand past anything the fixed composition could produce.
func checkEvidenceReadable(blob []byte) error {
	reader := flate.NewReader(bytes.NewReader(blob))
	inflated, err := io.Copy(io.Discard, io.LimitReader(reader, maxEvidenceInflatedBytes+1))
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if inflated > maxEvidenceInflatedBytes {
		return errEvidenceTooLargeInflated
	}
	return nil
}

// isBackfilled reports whether the alert is a retroactive finding. Absent means
// live, which is the safer reading: it keeps the narrow clock window.
func isBackfilled(msg *protocol.ControlMessage) bool {
	return msg.Backfilled != nil && *msg.Backfilled
}

// storedSeverity maps a wire severity to the spelling the store keeps. The wire
// vocabulary is the closed set — there is one, and this is only how it is
// written in the database.
func storedSeverity(severity *protocol.AlertSeverity) (alerts.Severity, bool) {
	if severity == nil || !protocol.ValidAlertSeverity(*severity) {
		return "", false
	}
	return alerts.Severity(strings.ToLower(string(*severity))), true
}

// severityLabel renders a severity for a log line, naming the absent case rather
// than logging an empty string that reads like a value.
func severityLabel(severity *protocol.AlertSeverity) string {
	if severity == nil {
		return "(absent)"
	}
	return string(*severity)
}

// alertRowID uses the id the device chose when it is one, and mints one when it
// is not. The device's id never decides whether a replay is a duplicate, so an
// unreadable one is a cosmetic loss rather than a reason to refuse the alert.
func alertRowID(deviceChosen string) uuid.UUID {
	if id, err := uuid.Parse(deviceChosen); err == nil {
		return id
	}
	return uuid.New()
}
