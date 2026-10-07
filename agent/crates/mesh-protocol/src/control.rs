use std::io::{Read, Write};

use flate2::read::DeflateDecoder;
use flate2::write::DeflateEncoder;
use flate2::Compression;
use serde::{Deserialize, Serialize};

use crate::error::ProtocolError;
use crate::types::{
    AgentCapability, FileEntry, KeyCode, LogEntry, MouseButton, NetworkInterface, Permissions,
    SessionToken,
};

/// Per-family anomaly rate inside an Edge Sentinel health summary.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct FamilyAnomalyRate {
    pub family: String,
    pub rate: f64,
}

/// Averaged metric dimension in an Edge Sentinel metric window.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct MetricDim {
    pub name: String,
    pub avg: f64,
}

/// Comparison direction for a declarative threshold-alert rule.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[non_exhaustive]
pub enum AlertComparator {
    /// Breaches while the metric is strictly greater than the threshold.
    Gt,
    /// Breaches while the metric is strictly less than the threshold.
    Lt,
    /// Breaches while the metric is greater than or equal to the threshold.
    Gte,
    /// Breaches while the metric is less than or equal to the threshold.
    Lte,
}

/// Every canonical vitals metric name a rule may watch.
pub const RULE_METRICS: [&str; 13] = [
    "cpu.total",
    "mem.used_percent",
    "disk.used_percent",
    "disk.mounts_critical",
    "net.rx_bps",
    "net.tx_bps",
    "stall.cpu.some",
    "stall.mem.some",
    "stall.mem.full",
    "stall.io.some",
    "stall.io.full",
    "disk.await_ms",
    "disk.queue_depth",
];

/// Alternate metric names a rule may use, each mapped to the canonical name it watches.
pub const RULE_METRIC_ALIASES: [(&str, &str); 2] = [
    ("mem.used", "mem.used_percent"),
    ("disk.used", "disk.used_percent"),
];

/// The longest window a rule may span, in seconds; it bounds the readings a predicate retains.
pub const MAX_RULE_WINDOW_SECS: u32 = 900;

/// The most extra conditions a rule may require alongside its own.
pub const MAX_RULE_TERMS: usize = 4;

// Compile-time floors: a window under a minute cannot state a trend, and a rule needs room
// for a second side.
const _: () = assert!(MAX_RULE_WINDOW_SECS >= 60);
const _: () = assert!(MAX_RULE_TERMS >= 1);

/// Resolves a rule's metric name to its canonical vitals name, or `None` outside the
/// vocabulary; such a rule never fires and is counted `unsupported`.
#[must_use]
pub fn canonical_rule_metric(name: &str) -> Option<&'static str> {
    if let Some(canonical) = RULE_METRICS.iter().find(|&&metric| metric == name) {
        return Some(canonical);
    }
    RULE_METRIC_ALIASES
        .iter()
        .find(|(alias, _)| *alias == name)
        .map(|(_, canonical)| *canonical)
}

/// How a rule derives the number it compares against its threshold.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[non_exhaustive]
pub enum RulePredicate {
    /// The reading itself, this second.
    #[default]
    Instant,
    /// Change per second across `window_secs`.
    Rate,
    /// The largest reading in the last `window_secs`.
    WindowMax,
    /// The mean reading over the last `window_secs`.
    WindowMean,
}

/// One extra condition a rule requires at the same instant as its own.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct RuleTerm {
    /// Watched dimension, resolved through [`canonical_rule_metric`].
    pub metric: String,
    /// Comparison direction.
    pub comparator: AlertComparator,
    /// Boundary this side must cross.
    pub threshold: f64,
    /// Hysteresis boundary this side must recover past; equal to `threshold`
    /// disables hysteresis on this side.
    pub clear: f64,
    /// How this side derives the number it compares.
    #[serde(default)]
    pub predicate: RulePredicate,
    /// Seconds this side's predicate spans. Zero for [`RulePredicate::Instant`].
    #[serde(default)]
    pub window_secs: u32,
}

/// A tenant-scoped threshold-alert rule evaluated locally each window; `clear` adds hysteresis.
/// Rules are data in a bounded grammar, so their cost is known before they reach an endpoint.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ThresholdRule {
    /// Stable rule id that attributes a breach and keeps state across an identical re-push.
    pub id: String,
    /// Definition revision; an alert's identity is `(device, rule, revision, window start)`.
    #[serde(default)]
    pub version: u32,
    /// How bad this rule's alerts are; stated on every alert the rule raises.
    #[serde(default)]
    pub severity: AlertSeverity,
    /// Watched dimension, resolved through [`canonical_rule_metric`]; unknown names never fire.
    pub metric: String,
    /// Comparison direction.
    pub comparator: AlertComparator,
    /// Fire boundary.
    pub threshold: f64,
    /// Hysteresis clear boundary on the safe side of `threshold`; equal to
    /// `threshold` disables hysteresis.
    pub clear: f64,
    /// Seconds the breach must hold continuously before it fires.
    pub sustain_secs: u32,
    /// How the compared number is derived from the metric; absent decodes as `Instant`.
    #[serde(default)]
    pub predicate: RulePredicate,
    /// Seconds the predicate spans. Zero for [`RulePredicate::Instant`], and at
    /// most [`MAX_RULE_WINDOW_SECS`] otherwise.
    #[serde(default)]
    pub window_secs: u32,
    /// Extra conditions that must hold at the same instant, at most
    /// [`MAX_RULE_TERMS`] of them. Empty is the plain single-dimension rule.
    #[serde(default)]
    pub all: Vec<RuleTerm>,
}

/// What one rule is doing on one device; a device reporting nothing is `unknown`, which only
/// the server can know.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[non_exhaustive]
pub enum RuleCoverageState {
    /// The rule is being evaluated on this device.
    #[default]
    Active,
    /// Cannot be evaluated here: unknown metric, out-of-bounds predicate, or no host reading.
    Unsupported,
    /// The rule exceeded its cost allowance on this device, so the device stopped running it.
    Throttled,
}

/// One rule's state on the device reporting it, carried additively in an
/// `AgentHealthSummary`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct RuleCoverage {
    /// Id of the [`ThresholdRule`] this describes.
    pub rule_id: String,
    /// What that rule is doing here.
    pub state: RuleCoverageState,
}

/// One currently-firing threshold-alert breach, carried additively in an `AgentHealthSummary`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct AlertBreach {
    /// Id of the [`ThresholdRule`] that fired.
    pub rule_id: String,
    /// Watched dimension, echoed for legibility without a rule-table join.
    pub metric: String,
    /// Metric value at the evaluation that reported the breach.
    pub value: f64,
}

/// Whether a `u32` field carries nothing, so it is omitted the way the server's
/// `omitempty` omits it and the two encoders stay byte-identical.
#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_zero_u32(value: &u32) -> bool {
    *value == 0
}

/// The same test for the timestamp fields.
#[allow(clippy::trivially_copy_pass_by_ref)]
fn is_zero_i64(value: &i64) -> bool {
    *value == 0
}

/// How bad an alert is; a closed set, and the server refuses a value outside it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[non_exhaustive]
pub enum AlertSeverity {
    /// Worth recording beside an incident, not worth raising one for.
    #[default]
    Info,
    /// Something is wrong and a person should look at it.
    Warning,
    /// Something is broken now.
    Critical,
}

/// One dimension the device's own correlation engine ranked at the moment an
/// alert fired, and how badly it broke pattern.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct RankedDim {
    /// The dimension's stable label, e.g. `disk.await_ms`.
    pub dim: String,
    /// The blended rank score in `[0, 1]`.
    pub score: f64,
}

/// One dimension's readings around the event at device resolution; central keeps a 60 s mean.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct EvidenceSeries {
    /// The dimension's stable label.
    pub dim: String,
    /// Readings across the event window, oldest first.
    pub points: Vec<HistoryPoint>,
}

/// Everything the device knows about why an alert fired; central cannot fetch it back later.
#[derive(Debug, Clone, PartialEq, Default, Serialize, Deserialize)]
pub struct AlertEvidence {
    /// Dimensions that broke pattern, most anomalous first.
    #[serde(default)]
    pub ranked: Vec<RankedDim>,
    /// Readings for the highest-ranked dimensions across the event window.
    #[serde(default)]
    pub series: Vec<EvidenceSeries>,
    /// What was running at the event instant.
    #[serde(default)]
    pub processes: Vec<EvidenceProcess>,
    /// Redacted host log lines from the event window.
    #[serde(default)]
    pub log_samples: Vec<String>,
    /// Whether the size cap cost this evidence anything. Always stated, so
    /// "nothing was dropped" and "nobody checked" never look alike.
    #[serde(default)]
    pub truncated: bool,
}

/// The codec `AlertEvidence` is compressed with, carried on every alert so a later codec is
/// additive.
pub const EVIDENCE_CODEC: &str = "deflate-1";

/// The most an alert's compressed evidence may weigh; excess is truncated and flagged.
pub const MAX_EVIDENCE_BYTES: usize = 64 * 1024;

impl AlertEvidence {
    /// Compresses this evidence under [`EVIDENCE_CODEC`], failing when it cannot be serialized
    /// or deflated.
    pub fn encode(&self) -> Result<Vec<u8>, ProtocolError> {
        let packed = rmp_serde::to_vec_named(self)?;
        let mut enc = DeflateEncoder::new(Vec::new(), Compression::default());
        enc.write_all(&packed)
            .and_then(|()| enc.finish())
            .map_err(|_| ProtocolError::CorruptEvidence)
    }

    /// Reads evidence written by [`AlertEvidence::encode`]; fails on an unknown `codec`, a blob
    /// that does not inflate, or content that is not evidence.
    pub fn decode(bytes: &[u8], codec: &str) -> Result<Self, ProtocolError> {
        if codec != EVIDENCE_CODEC {
            return Err(ProtocolError::UnknownEvidenceCodec(codec.to_string()));
        }
        let mut packed = Vec::new();
        DeflateDecoder::new(bytes)
            .read_to_end(&mut packed)
            .map_err(|_| ProtocolError::CorruptEvidence)?;
        Ok(rmp_serde::from_slice(&packed)?)
    }
}

/// One process running when an alert fired, as the device measured it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct EvidenceProcess {
    /// Position in the busiest-first list, from 1.
    pub rank: u32,
    /// Executable basename, never the full command line.
    pub basename: String,
    /// Hash of the full command line, present only on audited paths.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cmdline_hash: Option<String>,
    /// The operating system's identifier for the process.
    pub pid: u32,
    /// Share of the whole host's processors, 0–100; absent when the device could not measure it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cpu_share: Option<f64>,
    /// Resident memory, in bytes.
    pub mem: f64,
}

/// Sanitized process sample row for Edge Sentinel reporting.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ProcessReportEntry {
    pub rank: u32,
    pub basename: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cmdline_hash: Option<String>,
    pub pid: u32,
    pub cpu: f64,
    pub mem: f64,
}

/// Bounded health summary point returned for read-back requests.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct HealthSummary {
    pub ts: i64,
    pub tenant_id: String,
    pub node_anomaly_rate: f64,
    #[serde(default)]
    pub per_family_rates: Vec<FamilyAnomalyRate>,
    #[serde(default, with = "serde_bytes")]
    pub recent_bitmask: Vec<u8>,
    pub sampler_ver: String,
    pub model_ver: String,
}

/// Which central VictoriaMetrics tier a reconnect-backfill batch targets; 1 s raw is never pushed.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[non_exhaustive]
pub enum BackfillTier {
    /// 60 s windows rolled from local T0 (1 s) → VM raw tier, on the same grid
    /// the live stream emits on.
    #[default]
    Recent60s,
    /// 1 min points from local T1 → VM 1 min rollup.
    Rollup1m,
    /// 1 hr points from local T2 → VM 1 hr rollup.
    Rollup1h,
}

/// One pre-rolled historical sample replayed during reconnect backfill; central keeps `avg` only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct BackfillSample {
    pub name: String,
    pub ts: i64,
    pub value: f64,
}

/// One point of a history response scoped to a single dimension: timestamp in seconds, and value.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct HistoryPoint {
    pub ts: i64,
    pub value: f64,
}

/// One listening port: transport, number and owning process basename, never a bound address.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredPort {
    /// Transport, lowercase: `"tcp"` or `"udp"`.
    pub proto: String,
    /// Listening port number.
    pub port: u16,
    /// Basename of the owning process, or `""` when it cannot be resolved
    /// non-intrusively.
    pub process: String,
}

/// One host service on the endpoint, a systemd unit or Windows service: name and run state only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredService {
    /// Unit / service name (e.g. `"nginx.service"`, `"Spooler"`).
    pub name: String,
    /// Normalized run state (e.g. `"running"`, `"exited"`, `"failed"`,
    /// `"stopped"`).
    pub state: String,
}

/// One database engine inferred from a listening port and its owning process: family, version
/// and port only, never a connection string or credential.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredDbEngine {
    /// Engine family, lowercase (e.g. `"postgres"`, `"mysql"`, `"mongodb"`,
    /// `"redis"`).
    pub engine: String,
    /// Best-effort version string, or `""` when it is not determinable without
    /// an intrusive query.
    pub version: String,
    /// Port the engine listens on.
    pub port: u16,
}

/// One container found via a read-only local runtime: runtime, image, name and state only.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredContainer {
    /// Runtime, lowercase: `"docker"`, `"podman"`, or `"containerd"`.
    pub runtime: String,
    /// Image reference (repository[:tag]).
    pub image: String,
    /// Container name.
    pub name: String,
    /// Normalized state (e.g. `"running"`, `"exited"`, `"created"`).
    pub state: String,
}

/// One installed OS package, from dpkg/rpm or the Windows package registry: name and version.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DiscoveredPackage {
    /// Package name.
    pub name: String,
    /// Installed version string.
    pub version: String,
}

/// All control messages exchanged between agent and server, internally tagged so msgpack
/// output matches Go's flat struct.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type")]
#[non_exhaustive]
pub enum ControlMessage {
    // Agent → Server
    AgentRegister {
        capabilities: Vec<AgentCapability>,
        hostname: String,
        os: String,
        arch: String,
        version: String,
    },
    AgentHeartbeat {
        timestamp: i64,
    },
    AgentHealthSummary {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        tenant_id: String,
        #[serde(default)]
        node_anomaly_rate: f64,
        #[serde(default)]
        per_family_rates: Vec<FamilyAnomalyRate>,
        #[serde(default, with = "serde_bytes")]
        recent_bitmask: Vec<u8>,
        #[serde(default)]
        sampler_ver: String,
        #[serde(default)]
        model_ver: String,
        /// Threshold-alert breaches firing at build time; an older decoder ignores the field.
        #[serde(default)]
        breaches: Vec<AlertBreach>,
        /// What every installed rule is doing here; omitted when empty, which the server reads
        /// as the device having reported nothing.
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        rule_coverage: Vec<RuleCoverage>,
    },
    AgentMetricWindow {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        tenant_id: String,
        #[serde(default)]
        dims: Vec<MetricDim>,
    },
    ProcessReport {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        tenant_id: String,
        #[serde(default)]
        top_n: Vec<ProcessReportEntry>,
    },
    SessionAccept {
        token: SessionToken,
        relay_url: String,
    },
    SessionReject {
        token: SessionToken,
        reason: String,
    },

    // Server → Agent
    SessionRequest {
        token: SessionToken,
        relay_url: String,
        permissions: Permissions,
    },
    AgentUpdate {
        version: String,
        url: String,
        #[serde(default)]
        sha256: String,
        signature: String,
    },
    /// Agent acknowledges an update attempt (success or failure).
    AgentUpdateAck {
        version: String,
        success: bool,
        error: String,
    },

    // Bidirectional
    RelayReady,
    SwitchToWebRTC {
        sdp_offer: String,
    },
    SwitchAck,
    IceCandidate {
        candidate: String,
        mid: String,
    },

    // Input (browser → agent via relay)
    MouseMove {
        x: u16,
        y: u16,
    },
    MouseClick {
        button: MouseButton,
        pressed: bool,
        x: u16,
        y: u16,
    },
    KeyPress {
        key: KeyCode,
        pressed: bool,
    },
    TerminalResize {
        cols: u16,
        rows: u16,
    },

    // File operations
    FileListRequest {
        path: String,
    },
    FileListResponse {
        path: String,
        entries: Vec<FileEntry>,
    },
    FileListError {
        path: String,
        error: String,
    },
    FileDownloadRequest {
        path: String,
    },
    FileUploadRequest {
        path: String,
        total_size: u64,
    },

    // Chat
    ChatMessage {
        text: String,
        sender: String,
    },

    // Agent → Server: request an update check.
    RequestUpdate,

    // Server → Agent: response to RequestUpdate.
    UpdateCheckResponse {
        available: bool,
        version: String,
        url: String,
        sha256: String,
        signature: String,
    },

    // Agent → Server: request a short-lived chat authentication token.
    RequestChatToken {
        device_id: String,
    },

    // Server → Agent: chat token response.
    ChatTokenResponse {
        url: String,
        token: String,
        expires_at: String,
    },

    // Device lifecycle
    /// Server notifies agent that its device has been deleted; the agent cleans up and exits.
    AgentDeregistered {
        /// Informational; the server drops a zero-valued reason on encode, so absent decodes empty.
        #[serde(default)]
        reason: String,
    },

    /// Server requests agent to restart (exit code 42, systemd auto-restarts).
    RestartAgent {
        /// Informational; an absent reason decodes as empty and the control stream stays up.
        #[serde(default)]
        reason: String,
    },

    /// Server requests the agent to collect and send hardware inventory.
    RequestHardwareReport,

    /// Agent reports hardware inventory; `system_uuid` is the SMBIOS UUID the server joins AMT
    /// connections on, and is never returned over the API.
    HardwareReport {
        cpu_model: String,
        cpu_cores: u32,
        ram_total_mb: u64,
        disk_total_mb: u64,
        disk_free_mb: u64,
        network_interfaces: Vec<NetworkInterface>,
        #[serde(default)]
        system_uuid: String,
        #[serde(default)]
        amt_available: bool,
        #[serde(default)]
        amt_version: String,
    },

    /// Agent reports a hardware collection error.
    HardwareReportError {
        error: String,
    },

    /// Server requests the agent to collect and send log entries.
    RequestDeviceLogs {
        #[serde(default)]
        log_level: String,
        #[serde(default)]
        time_from: String,
        #[serde(default)]
        time_to: String,
        #[serde(default)]
        search: String,
        #[serde(default)]
        log_offset: u32,
        #[serde(default)]
        log_limit: u32,
        /// Host log source to query ("self", "journald", "windows"); empty selects the agent's files.
        #[serde(default)]
        source: String,
        /// Structured filter on the emitting unit (systemd unit or Windows
        /// provider). Empty matches every unit.
        #[serde(default)]
        unit: String,
    },

    /// Agent responds with log entries.
    DeviceLogsResponse {
        log_entries: Vec<LogEntry>,
        total_count: u32,
        has_more: bool,
        /// Distinct emitting units the host source offers, capped and sorted; empty means all units.
        #[serde(default)]
        available_units: Vec<String>,
    },

    /// Agent reports a log retrieval error.
    DeviceLogsError {
        error: String,
    },

    /// Server asks the agent for its bounded recent health summary window.
    RequestHealthWindow {
        #[serde(default)]
        since_ts: i64,
        #[serde(default)]
        limit: u32,
    },

    /// Agent responds with a bounded recent health summary window.
    HealthWindowResponse {
        #[serde(default)]
        summaries: Vec<HealthSummary>,
    },

    /// Agent → Server: request an admission slot to drain persisted history, with backlog hints
    /// (pending count, oldest timestamp).
    RequestBackfillSlot {
        #[serde(default)]
        pending_samples: u64,
        #[serde(default)]
        oldest_ts: i64,
    },

    /// Server → Agent: admission granted at `rate` samples/sec until `deadline` (unix seconds).
    /// Gated by the Backfill capability.
    GrantBackfill {
        #[serde(default)]
        rate: u32,
        #[serde(default)]
        deadline: i64,
    },

    /// Server → Agent: admission deferred; retry after `retry_after` seconds plus agent jitter.
    /// Gated by the Backfill capability.
    DeferBackfill {
        #[serde(default)]
        retry_after: u32,
    },

    /// Agent → Server: pre-rolled samples for one tier; `cursor` is the newest bucket timestamp,
    /// and the watermark advances only after the matching `MetricBackfillAck`.
    MetricBackfillBatch {
        #[serde(default)]
        tier: BackfillTier,
        #[serde(default)]
        samples: Vec<BackfillSample>,
        #[serde(default)]
        cursor: i64,
    },

    /// Server → Agent: durability ack that advances the per-tier watermark to `cursor`.
    /// Gated by the Backfill capability.
    MetricBackfillAck {
        #[serde(default)]
        tier: BackfillTier,
        #[serde(default)]
        cursor: i64,
    },

    /// Server → Agent: pull of full-resolution history for one dimension over a bounded window.
    /// Gated by the Backfill capability.
    RequestLocalHistory {
        #[serde(default)]
        dim: String,
        #[serde(default)]
        from_ts: i64,
        #[serde(default)]
        to_ts: i64,
        #[serde(default)]
        max_points: u32,
    },

    /// Agent → Server: response to `RequestLocalHistory`; `truncated` marks a cap at `max_points`.
    LocalHistoryResponse {
        #[serde(default)]
        dim: String,
        #[serde(default)]
        points: Vec<HistoryPoint>,
        #[serde(default)]
        truncated: bool,
    },

    /// Agent → Server: read-only host discovery profile; the server assigns the authoritative
    /// tenant, so `tenant_id` is empty. Gated by the Discovery capability.
    DiscoveryReport {
        #[serde(default)]
        ts: i64,
        #[serde(default)]
        tenant_id: String,
        #[serde(default)]
        ports: Vec<DiscoveredPort>,
        #[serde(default)]
        services: Vec<DiscoveredService>,
        #[serde(default)]
        db_engines: Vec<DiscoveredDbEngine>,
        #[serde(default)]
        containers: Vec<DiscoveredContainer>,
        #[serde(default)]
        packages: Vec<DiscoveredPackage>,
        #[serde(default)]
        truncated: bool,
    },

    /// Server → Agent: replace the active ruleset with the connecting agent's tenant-scoped rules.
    /// Gated by the ThresholdAlerts capability.
    PushAlertRules {
        #[serde(default)]
        rules: Vec<ThresholdRule>,
        /// Alerts this device may raise per rolling hour, enforced on the device; zero or absent
        /// keeps the current allowance.
        #[serde(default)]
        device_hourly_ceiling: u32,
    },

    /// Server → Agent: set the maintenance state, which pauses telemetry, discovery and
    /// alert evaluation; an absent `enabled` decodes as `false`.
    SetMaintenanceMode {
        #[serde(default)]
        enabled: bool,
    },

    /// Agent → Server: the maintenance state the agent actually reconciled to after
    /// `SetMaintenanceMode`.
    MaintenanceApplied {
        #[serde(default)]
        enabled: bool,
    },

    /// Agent → Server: one self-contained alert, identified by
    /// `(device, rule_id, rule_version, window_start_ts)`. Gated by the Alerts capability.
    AgentAlert {
        /// The device's own id for tracing one report; the server dedups on the tuple instead.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        alert_id: String,
        /// Which rule fired.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        rule_id: String,
        /// Which revision of that rule fired, counting from one.
        #[serde(default, skip_serializing_if = "is_zero_u32")]
        rule_version: u32,
        /// How bad the rule says this is.
        #[serde(default)]
        severity: AlertSeverity,
        /// Watched dimension, echoed so an alert reads without a rule-table join.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        metric: String,
        /// The value that crossed the line, when there was one.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        value: Option<f64>,
        /// Start of the window the rule decided on, in seconds; part of the alert's identity.
        #[serde(default, skip_serializing_if = "is_zero_i64")]
        window_start_ts: i64,
        /// End of that window, in seconds.
        #[serde(default, skip_serializing_if = "is_zero_i64")]
        window_end_ts: i64,
        /// When the device raised the alert, in seconds; a backfilled finding lies after its window.
        #[serde(default, skip_serializing_if = "is_zero_i64")]
        observed_ts: i64,
        /// Whether the device found this by re-running a rule over stored history.
        #[serde(default)]
        backfilled: bool,
        /// How `evidence` is compressed, e.g. `"deflate-1"`. Empty when the
        /// alert carries no evidence.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        evidence_codec: String,
        /// Compressed [`AlertEvidence`]; empty when the device had nothing to attach.
        #[serde(default, with = "serde_bytes", skip_serializing_if = "Vec::is_empty")]
        evidence: Vec<u8>,
    },

    /// Unknown future control message. Agents ignore this and keep the
    /// control stream alive; malformed frames still fail before this point.
    #[serde(other)]
    Unknown,
}
