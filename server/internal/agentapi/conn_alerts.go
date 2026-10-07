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

const (
	// alertEnvelopeHeadroomBytes covers the alert's own fields around its evidence.
	alertEnvelopeHeadroomBytes = 8 * 1024

	// maxAlertPayloadBytes is sized from the evidence cap so the largest evidence fits.
	maxAlertPayloadBytes = protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes

	// maxEvidenceInflatedBytes is the same inflated bound the read path applies.
	maxEvidenceInflatedBytes = protocol.MaxEvidenceInflatedBytes

	// maxBackfilledAlertBacklog is how far back a retroactive finding may reach.
	maxBackfilledAlertBacklog = 365 * 24 * time.Hour

	// fallbackGroupWindow is the shortest hold any shipped rule declares, so it only under-groups.
	fallbackGroupWindow = 15 * time.Minute

	// Each refusal cause is its own label.
	alertDropPayloadTooLarge      = "alert_payload_too_large"
	alertDropSeverityUnknown      = "alert_severity_unknown"
	alertDropIdentityIncomplete   = "alert_identity_incomplete"
	alertDropRuleUnknown          = "alert_rule_unknown"
	alertDropRuleStopped          = "alert_rule_stopped"
	alertDropTimestampOutOfRange  = "alert_timestamp_out_of_range"
	alertDropEvidenceCodecUnknown = "alert_evidence_codec_unknown"
	alertDropEvidenceUndecodable  = "alert_evidence_undecodable"
	alertDropOrganizationUnknown  = "alert_organization_unknown"
	alertDropOrganizationCeiling  = "alert_organization_ceiling"
	alertDropDuplicate            = "alert_duplicate"
)

// AlertRecorder files one alert into the room its rule groups it into and reports the outcome.
type AlertRecorder interface {
	Record(ctx context.Context, alert alerts.Alert, grouping alerts.Grouping) (alerts.Outcome, error)
}

// handleAgentAlert admits one alert and returns nil for a refused one, keeping the channel open.
func (a *AgentConn) handleAgentAlert(ctx context.Context, msg *protocol.ControlMessage, payloadLen int) error {
	if payloadLen > maxAlertPayloadBytes {
		a.dropTelemetry(alertDropPayloadTooLarge, "bytes", payloadLen, "max", maxAlertPayloadBytes)
		return nil
	}
	// Everything below either stores the alert or files exactly one typed drop.
	a.acceptedTelemetry(protocol.MsgAgentAlert)

	alert, ok := a.validatedAlert(msg)
	if !ok {
		return nil
	}

	// The write goes to a bounded slot so the read loop never waits on the store.
	a.persistTelemetry(ctx, 1, func(jobCtx context.Context, _ dbtx.Tenant) error {
		return a.storeAlert(jobCtx, alert)
	})
	return nil
}

// validatedAlert turns a control message into the alert to store, or files one typed drop.
func (a *AgentConn) validatedAlert(msg *protocol.ControlMessage) (alerts.Alert, bool) {
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
	if !a.shipsRule(msg.RuleID) {
		a.dropTelemetry(alertDropRuleUnknown, "rule_id", msg.RuleID)
		return alerts.Alert{}, false
	}
	// The machine's log reader keeps matching a stopped rule, so the stop is applied here.
	if !a.customerWants(msg.RuleID) {
		a.dropTelemetry(alertDropRuleStopped, "rule_id", msg.RuleID)
		return alerts.Alert{}, false
	}
	if !alertTimestampsInRange(msg, time.Now().UTC()) {
		a.dropTelemetry(alertDropTimestampOutOfRange,
			"window_start_ts", msg.WindowStartTS, "observed_ts", msg.ObservedTS,
			"backfilled", isBackfilled(msg))
		return alerts.Alert{}, false
	}
	// Evidence is optional, but a blob this build cannot read is refused.
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
		// The device's id is for cross-referencing the agent log and takes no part in identity.
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

// storeAlert resolves the machine's customer and files the alert, counting the outcome.
// A returned error hands the accounting back to the persist path, which counts the message as lost.
func (a *AgentConn) storeAlert(ctx context.Context, alert alerts.Alert) error {
	// A missing store is counted as a lost message, like a store that refused.
	if a.alertStore == nil {
		return errNoAlertStore
	}
	// Incident scoping keys are the customer's, so an unresolved customer drops the alert.
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

// groupingFor reads a rule's room scope and hold from its definition; an unknown rule gets the
// narrowest scope and shortest hold, which can only under-group.
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

// incidentScope is the narrowest tenancy rung the grouping keys name, defaulting to the device.
// Keys such as `mount` and `metric` describe the alert and leave the room unchanged.
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

// observeAlertOutcome counts stored rows per rule and files a typed drop for the outcomes that
// stored none; a spent budget also counts as suppression.
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

// customerWants reports whether the customer still receives alerts from the rule; only event
// rules are decided here, and a connection without the wanted set admits every rule.
func (a *AgentConn) customerWants(ruleID string) bool {
	if a.wantedEventRules == nil || !a.watchesEvents(ruleID) {
		return true
	}
	_, wanted := a.wantedEventRules[ruleID]
	return wanted
}

// watchesEvents reports whether the named rule reads the machine's own words.
func (a *AgentConn) watchesEvents(ruleID string) bool {
	if a.ruleCatalog == nil {
		return false
	}
	def, ok := a.ruleCatalog.Lookup(ruleID)
	return ok && def.WatchesEvents()
}

// shipsRule reports whether this build defines the rule; no catalogue admits every rule.
func (a *AgentConn) shipsRule(ruleID string) bool {
	if a.ruleCatalog == nil {
		return true
	}
	_, ok := a.ruleCatalog.Lookup(ruleID)
	return ok
}

// hasAlertIdentity reports whether rule, version and a forward-running window are present.
// With the device they identify the row a reconnect replay resolves to.
func hasAlertIdentity(msg *protocol.ControlMessage) bool {
	return msg.RuleID != "" &&
		msg.RuleVersion != 0 &&
		msg.WindowStartTS > 0 &&
		msg.WindowEndTS >= msg.WindowStartTS
}

// alertTimestampsInRange reports whether every timestamp is inside its window. The window start is
// part of the identity, so a bad one is refused; retroactive alerts reach back further.
func alertTimestampsInRange(msg *protocol.ControlMessage, now time.Time) bool {
	backlog := maxTelemetryBacklog
	if isBackfilled(msg) {
		backlog = maxBackfilledAlertBacklog
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

// isBackfilled reports whether the alert is a retroactive finding; absent means live.
func isBackfilled(msg *protocol.ControlMessage) bool {
	return msg.Backfilled != nil && *msg.Backfilled
}

// storedSeverity maps a wire severity to the spelling the store keeps.
func storedSeverity(severity *protocol.AlertSeverity) (alerts.Severity, bool) {
	if severity == nil || !protocol.ValidAlertSeverity(*severity) {
		return "", false
	}
	return alerts.Severity(strings.ToLower(string(*severity))), true
}

// severityLabel renders a severity for a log line, naming the absent case.
func severityLabel(severity *protocol.AlertSeverity) string {
	if severity == nil {
		return "(absent)"
	}
	return string(*severity)
}

// alertRowID uses the device's id when it parses as a UUID and mints one otherwise.
func alertRowID(deviceChosen string) uuid.UUID {
	if id, err := uuid.Parse(deviceChosen); err == nil {
		return id
	}
	return uuid.New()
}
