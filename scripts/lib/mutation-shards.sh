#!/usr/bin/env bash
# Single source of truth for mutation-test shard ids and Go mutation scope.
#
# Every Go shard runs `gremlins unleash .` from server/ so the coverage dry-run
# remains module-wide. The per-shard exclude regexp narrows only the source files
# mutated by that shard. Units are repository-relative to server/:
#   dir:<path>   every non-test Go source below a directory
#   file:<path>  one source file (used to split internal/api)
#
# scripts/tests/mutation-workflow.test.sh proves every non-test server Go source
# is covered by exactly one unit or by the global carve-outs. This prevents
# sources outside internal/ (notably tests/loadtest) from being mutated and
# counted once per shard.

# Where this library sits, so the functions below can read the gremlins config
# that is the single source of truth for the per-mutant leash.
MUTATION_SHARDS_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The Rust leg is split by scope, the same way the Go leg is: each shard names
# the behavior it mutates and owns a fixed set of sources.
#
# Naming the scope is what makes a red leg readable — "rust-core-alerts-retro
# failed" says which code lost coverage, where a slice number says only that one
# sixteenth of an interleaved list did. It is also what keeps a shard cheap:
# cargo-mutants rebuilds between mutants, so consecutive mutants inside one file
# reuse the incremental build, while an interleaved list rebuilds a different
# crate almost every time.
#
# Sizing is measured, not guessed. A shard's wall-clock is its mutant count times
# the per-mutant cost of its package, and that cost differs by an order of
# magnitude between packages: from completed runs, a mesh-agent-core mutant costs
# ~0.59 min (114 mutants in 70 min) against ~0.05 for edge-tsdb and ~0.12 for
# mesh-agent, because every mesh-agent-core mutant relinks the crate's 26 test
# binaries. Against the 90-minute cap, minus ~3 min of toolchain install and
# baseline build and 15 min of headroom, that puts a mesh-agent-core shard's
# ceiling near 120 mutants — which is why its ~1480 are split twenty ways
# while edge-tsdb's ~900 need three and mesh-agent and mesh-protocol one each.
#
# The `mutants` cargo profile (agent/Cargo.toml) drops debug info from that
# relink and takes roughly a fifth off the per-mutant cost; the ceiling above is
# stated without it, so it is headroom rather than budget already spent.
mutation_rust_shards() {
  # Not named `shards`: scripts/mutation-status-build.sh sources this library and
  # keeps a string by that name, and ShellCheck follows the source.
  local ids=(
    rust-core-ml-backfill-drain
    rust-core-ml-backfill-tiers
    rust-core-ml-sampling
    rust-core-ml-host-sources
    rust-core-ml-store-sink
    rust-core-ml-analysis
    rust-core-ml-redaction
    rust-core-alerts-retro-plan
    rust-core-alerts-retro-scan
    rust-core-alerts-conditions
    rust-core-alerts-evaluator
    rust-core-alerts-event
    rust-core-alerts-sink
    rust-core-correlate-divergence
    rust-core-correlate-ranking
    rust-core-session-terminal
    rust-core-session-dispatch
    rust-core-discovery
    rust-core-runtime-lifecycle
    rust-core-runtime
    rust-tsdb-blocks
    rust-tsdb-encoding
    rust-tsdb-substrates
    rust-agent-loops
    rust-protocol-wire
  )
  echo "${ids[*]}"
}

# The cargo package a shard mutates. cargo-mutants runs the tests of the mutated
# package only, so the package is also what the shard's per-mutant test cost is.
mutation_rust_shard_package() {
  case "$1" in
    rust-core-ml-backfill-drain | rust-core-ml-backfill-tiers) echo "mesh-agent-core" ;;
    rust-core-ml-sampling | rust-core-ml-host-sources | rust-core-ml-store-sink) echo "mesh-agent-core" ;;
    rust-core-ml-analysis | rust-core-ml-redaction) echo "mesh-agent-core" ;;
    rust-core-alerts-retro-plan | rust-core-alerts-retro-scan) echo "mesh-agent-core" ;;
    rust-core-alerts-conditions | rust-core-alerts-evaluator) echo "mesh-agent-core" ;;
    rust-core-alerts-event | rust-core-alerts-sink) echo "mesh-agent-core" ;;
    rust-core-correlate-divergence | rust-core-correlate-ranking) echo "mesh-agent-core" ;;
    rust-core-session-terminal | rust-core-session-dispatch) echo "mesh-agent-core" ;;
    rust-core-discovery | rust-core-runtime-lifecycle | rust-core-runtime) echo "mesh-agent-core" ;;
    rust-tsdb-blocks | rust-tsdb-encoding | rust-tsdb-substrates) echo "edge-tsdb" ;;
    rust-agent-loops) echo "mesh-agent" ;;
    rust-protocol-wire) echo "mesh-protocol" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# Sources a shard owns, as units repository-relative to agent/:
#   dir:<path>   every source below a directory
#   file:<path>  one source file
# The literal `rest` marks a package's catch-all shard, which owns everything its
# siblings do not claim. Every package has exactly one, so a source added
# tomorrow is mutated the day it lands instead of falling through the map.
mutation_rust_shard_units() {
  case "$1" in
    rust-core-ml-backfill-drain)
      echo "file:crates/mesh-agent-core/src/ml/backfill/drain.rs"
      ;;
    rust-core-ml-backfill-tiers)
      echo "file:crates/mesh-agent-core/src/ml/backfill/mod.rs"
      ;;
    rust-core-ml-sampling)
      echo "file:crates/mesh-agent-core/src/ml/sampler.rs file:crates/mesh-agent-core/src/ml/host_metric_stream.rs file:crates/mesh-agent-core/src/ml/window.rs"
      ;;
    rust-core-ml-host-sources)
      echo "file:crates/mesh-agent-core/src/ml/diskperf.rs file:crates/mesh-agent-core/src/ml/pressure.rs file:crates/mesh-agent-core/src/ml/primary_iface.rs file:crates/mesh-agent-core/src/ml/cgroup.rs"
      ;;
    rust-core-ml-store-sink)
      echo "file:crates/mesh-agent-core/src/ml/store_sink.rs"
      ;;
    rust-core-ml-analysis)
      echo "file:crates/mesh-agent-core/src/ml/kmeans.rs file:crates/mesh-agent-core/src/ml/ensemble.rs file:crates/mesh-agent-core/src/ml/mod.rs"
      ;;
    rust-core-ml-redaction)
      echo "file:crates/mesh-agent-core/src/ml/redact.rs"
      ;;
    rust-core-alerts-retro-plan)
      echo "file:crates/mesh-agent-core/src/alerts/retro/mod.rs"
      ;;
    rust-core-alerts-retro-scan)
      echo "file:crates/mesh-agent-core/src/alerts/retro/scan.rs"
      ;;
    rust-core-alerts-conditions)
      echo "file:crates/mesh-agent-core/src/alerts/evaluator/condition.rs"
      ;;
    rust-core-alerts-evaluator)
      echo "file:crates/mesh-agent-core/src/alerts/evaluator/mod.rs"
      ;;
    rust-core-alerts-event)
      echo "file:crates/mesh-agent-core/src/alerts/event.rs file:crates/mesh-agent-core/src/alerts/mod.rs"
      ;;
    rust-core-alerts-sink)
      echo "file:crates/mesh-agent-core/src/alerts/sink.rs file:crates/mesh-agent-core/src/alerts/evidence.rs"
      ;;
    rust-core-correlate-divergence) echo "file:crates/mesh-agent-core/src/correlate/ks.rs" ;;
    rust-core-correlate-ranking)
      echo "file:crates/mesh-agent-core/src/correlate/rank.rs file:crates/mesh-agent-core/src/correlate/window.rs file:crates/mesh-agent-core/src/correlate/mod.rs"
      ;;
    rust-core-session-terminal)
      echo "file:crates/mesh-agent-core/src/session/terminal_handle.rs"
      ;;
    rust-core-session-dispatch)
      echo "dir:crates/mesh-agent-core/src/session/handlers file:crates/mesh-agent-core/src/session/handler.rs file:crates/mesh-agent-core/src/session/relay.rs file:crates/mesh-agent-core/src/session/mod.rs"
      ;;
    rust-core-discovery) echo "dir:crates/mesh-agent-core/src/discovery" ;;
    rust-core-runtime-lifecycle)
      echo "file:crates/mesh-agent-core/src/update.rs file:crates/mesh-agent-core/src/maintenance.rs file:crates/mesh-agent-core/src/identity.rs file:crates/mesh-agent-core/src/platform.rs file:crates/mesh-agent-core/src/terminal.rs"
      ;;
    rust-core-runtime) echo "rest" ;;
    rust-tsdb-blocks)
      echo "dir:crates/edge-tsdb/src/store file:crates/edge-tsdb/src/compact.rs file:crates/edge-tsdb/src/deflate.rs"
      ;;
    rust-tsdb-encoding)
      echo "file:crates/edge-tsdb/src/gorilla.rs file:crates/edge-tsdb/src/bitio.rs file:crates/edge-tsdb/src/crc.rs file:crates/edge-tsdb/src/frame.rs file:crates/edge-tsdb/src/tier.rs"
      ;;
    rust-tsdb-substrates) echo "rest" ;;
    rust-agent-loops) echo "rest" ;;
    rust-protocol-wire) echo "rest" ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

mutation_rust_unit_matches() {
  local unit="${1:?mutation unit required}"
  local source="${2:?source path required}"

  case "$unit" in
    dir:*)
      local dir="${unit#dir:}"
      [[ "$source" == "$dir/"* ]]
      ;;
    file:*) [[ "$source" == "${unit#file:}" ]] ;;
    *) return 1 ;;
  esac
}

# A unit as a cargo-mutants path glob. Globs containing a slash match the whole
# workspace-relative path, which every unit here does.
mutation_rust_shard_glob() {
  local unit="${1:?mutation unit required}"

  case "$unit" in
    dir:*) printf '%s/**' "${unit#dir:}" ;;
    file:*) printf '%s' "${unit#file:}" ;;
    *)
      echo "unknown mutation unit: $unit" >&2
      return 1
      ;;
  esac
}

# The cargo-mutants arguments selecting one shard's mutants, one per line so a
# caller can read them into an array without re-splitting paths.
mutation_rust_shard_args() {
  local shard="${1:?mutation shard required}"
  local pkg units other other_units unit

  pkg="$(mutation_rust_shard_package "$shard")" || return 1
  units="$(mutation_rust_shard_units "$shard")" || return 1
  printf -- '--package\n%s\n' "$pkg"

  if [ "$units" = "rest" ]; then
    for other in $(mutation_rust_shards); do
      [ "$other" = "$shard" ] && continue
      [ "$(mutation_rust_shard_package "$other")" = "$pkg" ] || continue
      other_units="$(mutation_rust_shard_units "$other")"
      [ "$other_units" = "rest" ] && continue
      for unit in $other_units; do
        printf -- '--exclude\n%s\n' "$(mutation_rust_shard_glob "$unit")"
      done
    done
    return 0
  fi

  for unit in $units; do
    printf -- '--file\n%s\n' "$(mutation_rust_shard_glob "$unit")"
  done
}

# What one shard is allowed to project to, in minutes. The job cap is 90; a run
# pays about 3 of those for the toolchain install and the unmutated baseline, and
# 15 are held back as headroom, because a shard sized to finish at 89 minutes is
# a shard that fails the first time a runner is slow.
mutation_rust_shard_budget_minutes() {
  echo 72
}

# The measured cost of one mutant, in thousandths of a minute, by package.
#
# These come from completed nightly shards — mesh-agent-core's 114-mutant alerts
# leg in 70 min, edge-tsdb's 350 in 17.8, mesh-agent's 264 in 32, mesh-protocol's
# 83 in 6 — each then scaled by the 0.78 the `mutants` cargo profile takes off a
# rebuild (agent/Cargo.toml). The spread is not noise: cargo-mutants relinks the
# mutated package's test binaries once per mutant, and mesh-agent-core has 26 of
# them, so a mutant there costs an order of magnitude more than one in edge-tsdb.
#
# Re-measure from a nightly run rather than adjusting these to make a shard fit.
mutation_rust_package_milliminutes_per_mutant() {
  case "$1" in
    mesh-agent-core) echo 460 ;;
    edge-tsdb) echo 40 ;;
    mesh-agent) echo 94 ;;
    mesh-protocol) echo 56 ;;
    *)
      echo "unknown mutation package: $1" >&2
      return 1
      ;;
  esac
}

mutation_web_shards() {
  echo "web"
}

mutation_go_shards() {
  echo "go-api-runtime go-api-intake go-api-status go-api-converters go-api-identity go-api-tenancy-admin go-api-device-control go-api-device-sessions go-api-device-reads go-api-incidents go-api-rules go-api-enrollment go-api-updates-purge go-agentapi-connection go-agentapi-handshake go-agentapi-backfill go-agentapi-edge-telemetry go-domain-rules go-domain-alerts-room go-domain-alerts-record go-domain-persistence go-amt go-updates-certificates go-protocol-wire go-relay-signaling go-observability-harness go-composition-root"
}

mutation_all_shards() {
  echo "$(mutation_rust_shards) $(mutation_go_shards) $(mutation_web_shards)"
}

# A CLI -E overrides server/.gremlins.yaml exclude-files, so every sharded run
# must restate the generated code, entry points, and shared test scaffolding.
#
# cmd/meshserver is excluded as a package rather than as one file: every source
# in it is the process's own wiring — flag parsing, construction order, and the
# goroutines that start the periodic workers — and a mutant there changes how the
# process is assembled rather than what any behavior does. Splitting that wiring
# across files for readability must not quietly enrol it in mutation testing.
mutation_go_global_excludes() {
  echo 'openapi_gen\.go|cmd/meshserver/|tests/loadtest/main\.go|internal/testutil/|internal/faulttest/'
}

mutation_go_shard_units() {
  case "$1" in
    go-api-runtime)
      echo "file:internal/api/api.go file:internal/api/middleware.go file:internal/api/wsconn.go file:internal/api/ratelimit.go"
      ;;
    # What the server accepts from a caller, and what it refuses to write into
    # a log once it has.
    go-api-intake)
      echo "file:internal/api/validate.go file:internal/api/log_redact.go"
      ;;
    # What the server says about itself: the browser's error reports, the
    # liveness answer, and the readings assembled for the metrics page.
    go-api-status)
      echo "file:internal/api/handlers_client_errors.go file:internal/api/handlers_health.go file:internal/api/metrics_assemble.go"
      ;;
    go-api-converters)
      echo "file:internal/api/converters.go file:internal/api/converters_incidents.go file:internal/api/converters_rules.go"
      ;;
    go-api-identity)
      echo "file:internal/api/handlers_auth.go file:internal/api/handlers_users.go file:internal/api/handlers_security_groups.go file:internal/api/handlers_security_group_members.go file:internal/api/handlers_audit.go"
      ;;
    go-api-tenancy-admin)
      echo "file:internal/api/handlers_organizations.go file:internal/api/handlers_sites.go file:internal/api/handlers_device_tags.go file:internal/api/handlers_alert_limits.go file:internal/api/handlers_push.go"
      ;;
    # Acting on a machine: listing it, ordering it about, and taking it out of
    # service. Every mutant here re-pays the Postgres-backed API suite, which is
    # what makes these the most expensive mutants in the module and why the
    # sessions half sits in its own shard.
    go-api-device-control)
      echo "file:internal/api/handlers_devices.go file:internal/api/handlers_device_actions.go file:internal/api/handlers_maintenance.go"
      ;;
    # Reaching a machine: the out-of-band power path, the session a technician
    # opens, and the relay that carries it.
    go-api-device-sessions)
      echo "file:internal/api/handlers_amt.go file:internal/api/handlers_sessions.go file:internal/api/handlers_relay.go"
      ;;
    go-api-device-reads)
      echo "file:internal/api/handlers_device_summary.go file:internal/api/handlers_device_inventory.go file:internal/api/handlers_device_metrics.go file:internal/api/handlers_device_history.go"
      ;;
    go-api-incidents)
      echo "file:internal/api/handlers_incidents.go file:internal/api/handlers_incident_moves.go file:internal/api/handlers_incident_evidence.go"
      ;;
    go-api-rules)
      echo "file:internal/api/handlers_rules.go file:internal/api/handlers_rules_admin.go file:internal/api/handlers_rules_read.go file:internal/api/handlers_rules_tuning.go file:internal/api/handlers_rules_rollout.go"
      ;;
    go-api-enrollment)
      echo "file:internal/api/handlers_enrollment.go file:internal/api/handlers_install.go"
      ;;
    go-api-updates-purge)
      echo "file:internal/api/handlers_updates.go file:internal/api/handlers_purge.go"
      ;;
    go-agentapi-connection)
      echo "file:internal/agentapi/conn.go file:internal/agentapi/conn_register.go file:internal/agentapi/conn_guard.go file:internal/agentapi/conn_maintenance.go file:internal/agentapi/server.go file:internal/agentapi/server_connection.go file:internal/agentapi/deregister.go"
      ;;
    go-agentapi-handshake)
      echo "file:internal/agentapi/handshaker.go file:internal/agentapi/errors.go"
      ;;
    go-agentapi-backfill)
      echo "file:internal/agentapi/backfill_scheduler.go file:internal/agentapi/conn_backfill.go"
      ;;
    go-agentapi-edge-telemetry)
      echo "file:internal/agentapi/conn_discovery.go file:internal/agentapi/conn_telemetry.go file:internal/agentapi/conn_accounting.go file:internal/agentapi/conn_coverage.go file:internal/agentapi/conn_logs.go file:internal/agentapi/conn_history.go file:internal/agentapi/conn_hardware.go file:internal/agentapi/alert_breach.go file:internal/agentapi/alert_rules.go file:internal/agentapi/conn_alerts.go file:internal/agentapi/alert_rules_catalogue.go file:internal/agentapi/vitals.go"
      ;;
    # What a rule says, and what happens when one fires. They were one shard
    # until the pair grew past a single job; they are two questions anyway —
    # whether a rule resolves to what its author meant, and whether a breach
    # becomes the right alert.
    go-domain-rules)
      echo "dir:internal/rules"
      ;;
    # Split by what the mutants cost, along the seam the package doc already
    # draws: the room reports fold into, and the record they fold from.
    go-domain-alerts-room)
      echo "file:internal/alerts/room.go file:internal/alerts/queue.go file:internal/alerts/incident.go file:internal/alerts/aggregate.go"
      ;;
    go-domain-alerts-record)
      echo "file:internal/alerts/types.go file:internal/alerts/limits.go file:internal/alerts/noise.go file:internal/alerts/postgres.go file:internal/alerts/postgres_fold.go file:internal/alerts/postgres_lifecycle.go file:internal/alerts/postgres_limits.go file:internal/alerts/postgres_noise.go file:internal/alerts/postgres_retention.go"
      ;;
    go-domain-persistence)
      echo "dir:internal/auth dir:internal/db dir:internal/dbtx dir:internal/device dir:internal/inventory dir:internal/lifecycle dir:internal/organization dir:internal/settings dir:internal/session dir:internal/audit dir:internal/usecase"
      ;;
    go-amt)
      echo "dir:internal/amt"
      ;;
    go-updates-certificates)
      echo "dir:internal/updater dir:internal/cert dir:internal/notifications"
      ;;
    # The bytes on the wire, and the host details that go into them.
    go-protocol-wire)
      echo "dir:internal/protocol dir:internal/osutil"
      ;;
    # Carrying a live session between a browser and a machine: the relay itself,
    # the negotiation that sets it up, and the browser-facing surface.
    go-relay-signaling)
      echo "dir:internal/relay dir:internal/signaling dir:internal/clientapi"
      ;;
    go-observability-harness)
      echo "dir:internal/telemetry dir:internal/metrics dir:internal/testpg dir:internal/testvm dir:internal/testreaper dir:tests/loadtest"
      ;;
    # The composition root. It is mutated rather than carved out: a dry run
    # measured 23 mutants there, and what they land on is behavior a test can
    # state — the refusals that name a missing dependency, the all-or-nothing
    # wiring of the metrics store's four faces, and the fallback that leaves
    # device deletion as a plain Postgres delete when there are no series to
    # purge. The acceptance suite stands the assembly up, so a mutant that
    # unwires a port has somewhere to be killed.
    go-composition-root)
      echo "dir:internal/app"
      ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# What one Go mutant costs the shard that owns it, in seconds.
#
# gremlins re-runs the test packages that cover the mutated line, so the cost is
# a property of those tests rather than of the source: an internal/api handler
# mutant re-pays the Postgres-backed API suite and the integration tests that
# reach it (~42-72 s), while an internal/agentapi mutant re-runs an in-process
# harness (~3-10 s). Ignoring that spread is what makes a shard count meaningless
# as a size — 56 API mutants and 640 harness mutants are both under an hour.
#
# These come from completed nightly shards, rounded up so a shard is never
# projected cheaper than it ran. Re-measure from a run rather than lowering a
# number to make a shard fit.
#
# Measure over the mutants that FINISH, not over elapsed_time / mutants_total.
# A mutant that never terminates holds a worker for its whole leash, and dividing
# that leash across the shard's mutants charges every one of them for it: it
# reported go-updates-certificates at 21 s a mutant when 143 of its 144 finished
# in 106 seconds, and go-amt at 13 when its real figure is under 2. The window to
# divide is from the end of the coverage run to the last result that is not
# TIMED OUT; the blocked ones are counted separately, below.
mutation_go_shard_seconds_per_mutant() {
  case "$1" in
    go-api-runtime) echo 46 ;;
    go-api-intake) echo 42 ;;
    go-api-status) echo 46 ;;
    go-api-converters) echo 68 ;;
    go-api-incidents) echo 63 ;;
    go-api-rules) echo 67 ;;
    go-api-identity) echo 57 ;;
    go-api-tenancy-admin) echo 72 ;;
    go-api-enrollment) echo 53 ;;
    go-api-updates-purge) echo 64 ;;
    go-api-device-control) echo 69 ;;
    go-api-device-sessions) echo 66 ;;
    go-api-device-reads) echo 54 ;;
    go-agentapi-connection) echo 10 ;;
    go-agentapi-handshake) echo 8 ;;
    go-agentapi-edge-telemetry) echo 7 ;;
    go-agentapi-backfill) echo 3 ;;
    go-domain-rules) echo 12 ;;
    # The room half carries the reads a person does inside an incident, each of
    # which assembles a room from several tables; the record half is a narrower
    # write path over one.
    go-domain-alerts-room) echo 39 ;;
    go-domain-alerts-record) echo 25 ;;
    go-domain-persistence) echo 5 ;;
    # These two spent almost their whole run inside one blocked mutant's leash:
    # 224 of go-amt's 225 mutants finished in 5 minutes, 143 of
    # go-updates-certificates' 144 in under two. What they cost is the leash
    # declared below, not these figures.
    go-amt) echo 2 ;;
    go-updates-certificates) echo 1 ;;
    # Neither leaves the process: the wire codecs and the signalling handshake
    # run against in-memory fixtures.
    go-protocol-wire | go-relay-signaling) echo 1 ;;
    go-observability-harness) echo 1 ;;
    # Postgres-backed like the API shards: every mutant re-pays a schema
    # migration and a full assembly.
    go-composition-root) echo 5 ;;
    *)
      echo "unknown mutation shard: $1" >&2
      return 1
      ;;
  esac
}

# How many of a shard's mutants never terminate.
#
# Each of these is a loop whose exit condition CONDITIONALS_NEGATION removes, so
# the mutated build runs until gremlins' own deadline cuts it off. gremlins
# records that as TIMED OUT, which is neither a kill nor a survivor — it leaves
# the score alone and takes the wall clock instead, which is why nothing in a
# report ever said these were there.
#
# They are a property of the code, so they are declared beside the per-mutant
# cost and re-measured the same way: count the TIMED OUT lines in a shard's run.
mutation_go_shard_blocking_mutants() {
  case "$1" in
    # server.go's listener guard and server_connection.go's read guard: a mutant
    # that stops the server publishing its address leaves every test that dials
    # it waiting.
    go-agentapi-connection) echo 2 ;;
    # transport/mps.go's accept loop returns on a cancelled context; negated, it
    # accepts forever.
    go-amt) echo 1 ;;
    # notifications/vapid.go pads a private key to 32 bytes; negated, it prepends
    # zero bytes without end.
    go-updates-certificates) echo 1 ;;
    # postgres_retention.go's drain repeats a batched delete until a pass
    # reclaims less than a full batch; negated, a pass that reclaims nothing
    # repeats forever. This is the mutant that took run 33727909504 past the cap.
    go-domain-alerts-record) echo 1 ;;
    *)
      mutation_go_shard_seconds_per_mutant "$1" >/dev/null || return 1
      echo 0
      ;;
  esac
}

# The longest a Go shard's coverage run takes before the first mutant runs.
# Measured across all 26 Go shards of a nightly: 185s at the fastest, 298s at the
# slowest. gremlins derives every mutant's leash from this same figure, so it
# sets both the shard's fixed overhead and the leash ceiling below.
mutation_go_coverage_elapsed_ceiling_seconds() {
  echo 300
}

# What a Go shard pays before the first mutant runs, beyond the coverage run:
# checkout, the image pulls, Postgres, VictoriaMetrics, the toolchain and
# installing gremlins. Measured at 26s.
mutation_go_setup_ceiling_seconds() {
  echo 30
}

# The most wall clock a single non-terminating mutant can cost a shard.
#
# gremlins multiplies the coverage run's elapsed time by the timeout coefficient
# and gives every mutant that long, so the coefficient is not a tuning knob — it
# is the bound on this number. server/.gremlins.yaml is where it is set, and this
# reads it there rather than restating it.
mutation_go_leash_ceiling_seconds() {
  local coefficient
  coefficient="$(sed -nE 's/^[[:space:]]*timeout-coefficient:[[:space:]]*([0-9]+).*/\1/p' \
    "$MUTATION_SHARDS_LIB_DIR/../../server/.gremlins.yaml")"
  case "$coefficient" in
    '' | *[!0-9]*)
      echo "could not read timeout-coefficient from server/.gremlins.yaml" >&2
      return 1
      ;;
  esac
  echo $((coefficient * $(mutation_go_coverage_elapsed_ceiling_seconds)))
}

# Minutes of mutant execution a Go shard may spend.
#
# The 90-minute cap is spent in four parts, and the budget is what is left after
# the other three: 30s of setup, up to 300s of coverage, the budget itself, and a
# headroom equal to one full leash — so a mutant that starts blocking between one
# nightly and the next, before any run has declared it, still cannot carry the
# job past the cap on its own.
#
#   90:00 - 0:30 setup - 5:00 coverage - 15:00 leash = 69:30
mutation_go_shard_budget_minutes() {
  echo 69
}

# Per-shard gremlins timeout-coefficient override. Every shard inherits the
# baseline in server/.gremlins.yaml today (empty output => no CLI flag), because
# the baseline is itself held to a bound a blocked mutant cannot break: see
# mutation_go_leash_ceiling_seconds.
#
# The seam stays for a shard that one day needs a tighter leash than the rest.
# What it may never carry is a looser one — a coefficient above the baseline puts
# that shard's blocked mutants back outside the bound the budget is built on.
#
# A coefficient is a bound, not a cure: where a mutant blocks because the test
# harness has no deadline of its own, the deadline is what to fix — a bounded
# harness kills the same mutant in seconds instead of minutes.
mutation_go_shard_timeout_coefficient() {
  case "$1" in
    *) echo "" ;;
  esac
}

mutation_go_unit_matches() {
  local unit="${1:?mutation unit required}"
  local source="${2:?source path required}"

  case "$unit" in
    dir:*)
      local dir="${unit#dir:}"
      [[ "$source" == "$dir/"* ]]
      ;;
    file:*) [[ "$source" == "${unit#file:}" ]] ;;
    *) return 1 ;;
  esac
}

mutation_go_unit_regex() {
  local unit="${1:?mutation unit required}"
  local path

  case "$unit" in
    dir:*) path="${unit#dir:}/" ;;
    file:*) path="${unit#file:}" ;;
    *)
      echo "unknown mutation unit: $unit" >&2
      return 1
      ;;
  esac

  case "$path" in
    *[!a-zA-Z0-9_./-]*)
      echo "unsupported character in mutation unit: $unit" >&2
      return 1
      ;;
  esac
  path="${path//./\\.}"
  if [[ "$unit" == file:* ]]; then
    path="$path\$"
  fi
  printf '%s' "$path"
}

mutation_go_shard_exclude_regex() {
  local shard="${1:?mutation shard required}"
  local other unit part
  local regex

  mutation_go_shard_units "$shard" >/dev/null || return 1
  regex="$(mutation_go_global_excludes)"
  for other in $(mutation_go_shards); do
    [[ "$other" == "$shard" ]] && continue
    for unit in $(mutation_go_shard_units "$other"); do
      part="$(mutation_go_unit_regex "$unit")" || return 1
      regex="$regex|$part"
    done
  done
  printf '%s\n' "$regex"
}
