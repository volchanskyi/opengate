package metrics

// The counters below record outcomes from a closed set, and every value of each
// set is published at zero from start-up. A counter created on its first
// increment has no reading before it, so a rate over it misses the first event
// after every start, and a rule watching for an outcome reads nothing until the
// outcome it watches for has happened.

// The outcomes a raw-log pull is recorded under: the pull succeeded (the
// audited count), the device was not connected, its agent does not offer
// logs, another pull was in flight, it ran out of time, or it failed.
const (
	LogPullOK          = "ok"
	LogPullOffline     = "offline"
	LogPullUnsupported = "unsupported"
	LogPullBusy        = "busy"
	LogPullTimeout     = "timeout"
	LogPullError       = "error"
)

var deviceLogPullOutcomes = []string{
	LogPullOK, LogPullOffline, LogPullUnsupported, LogPullBusy, LogPullTimeout, LogPullError,
}

// DeviceLogPullOutcomes returns the outcomes a raw-log pull is recorded under,
// each published at zero from start-up.
func DeviceLogPullOutcomes() []string {
	return append([]string(nil), deviceLogPullOutcomes...)
}

// alertSuppressionReasons are the reasons an alert that reached the server is
// refused a stored row. The value is the alert store's own outcome.
var alertSuppressionReasons = []string{"organization_ceiling"}

// AlertSuppressionReasons returns the reasons an alert is refused a stored row,
// each published at zero from start-up.
func AlertSuppressionReasons() []string {
	return append([]string(nil), alertSuppressionReasons...)
}

// edgeTelemetryDropReasons are the reasons a connection discards telemetry: an
// admission bound, an empty or unreadable payload, a device with no tenant or
// one being removed, a store that failed or had no free slot, and each way an
// alert is refused. The connection spells them where it drops.
var edgeTelemetryDropReasons = []string{
	"payload_too_large", "interval_floor", "tenant_missing", "persist_failed", "persist_slots_full",
	"tombstoned", "unknown_family", "unknown_dim",
	"empty_summary", "empty_summaries", "empty_dims", "empty_processes", "empty_discovery",
	"discovery_payload_too_large", "discovery_interval_floor", "backfill_out_of_retention",
	"alert_payload_too_large", "alert_severity_unknown", "alert_identity_incomplete",
	"alert_rule_unknown", "alert_rule_stopped", "alert_timestamp_out_of_range",
	"alert_evidence_codec_unknown", "alert_evidence_undecodable",
	"alert_organization_unknown", "alert_organization_ceiling", "alert_duplicate",
}

// EdgeTelemetryDropReasons returns the reasons telemetry is discarded under, each
// published at zero from start-up.
func EdgeTelemetryDropReasons() []string {
	return append([]string(nil), edgeTelemetryDropReasons...)
}

// edgeTelemetryIngestTypes are the message types a connection counts as
// accepted telemetry. Each is the protocol's own name for the message.
var edgeTelemetryIngestTypes = []string{
	"AgentHealthSummary", "AgentMetricWindow", "ProcessReport",
	"HealthWindowResponse", "DiscoveryReport", "AgentAlert",
}

// EdgeTelemetryIngestTypes returns the message types counted as ingested
// telemetry, each published at zero from start-up.
func EdgeTelemetryIngestTypes() []string {
	return append([]string(nil), edgeTelemetryIngestTypes...)
}

// The two answers the catch-up scheduler gives a reconnecting machine.
const (
	backfillGrant = "grant"
	backfillDefer = "defer"
)

// seedOutcomes publishes every value of every closed set above at zero.
func (m *Metrics) seedOutcomes() {
	for _, outcome := range deviceLogPullOutcomes {
		m.DeviceLogPullsTotal.WithLabelValues(outcome)
	}
	for _, reason := range alertSuppressionReasons {
		m.AlertsSuppressedTotal.WithLabelValues(reason)
	}
	for _, reason := range edgeTelemetryDropReasons {
		m.EdgeTelemetryDropsTotal.WithLabelValues(reason)
	}
	for _, msgType := range edgeTelemetryIngestTypes {
		m.EdgeTelemetryIngestedTotal.WithLabelValues(msgType)
	}
	m.EdgeBackfillDecisionsTotal.WithLabelValues(backfillGrant)
	m.EdgeBackfillDecisionsTotal.WithLabelValues(backfillDefer)
}
