#!/usr/bin/env bash
# OCI Bastion session wrapper. Creates (or reuses) a Managed SSH session to the
# OKE worker node and either opens an interactive shell (`ssh`) or runs
# read-only sanity checks (`diagnose`).
#
# Invoked via the root Makefile's `make ssh` target. (`make tunnel` moved to
# `kubectl port-forward` post-cutover — the monitoring UIs are ClusterIP
# services, not host ports reachable over the bastion.)
#
# Caching: the active session OCID + canonical ssh-metadata.command +
# expiry are persisted at ~/.cache/opengate/bastion-session.json.
# Subsequent invocations within the session TTL skip the 60-90s
# session-create round trip.
#
# Persistent log: every run appends to ~/.cache/opengate/bastion-session.log
# (rolling — old entries trimmed at ~5 MB). Inspect after a failure with
# `tail -n 100 ~/.cache/opengate/bastion-session.log`.
#
# Subcommands:
#   ssh          interactive shell on the OKE worker node
#   diagnose     read-only checks: bastion state, plugin status, active
#                sessions on the bastion, cache state. No state changes.
#   purge        delete the local cache file; next run creates a fresh
#                session.
#
# Env knobs:
#   OPENGATE_BASTION_DEBUG=1       # set -x + OCI CLI --debug; very verbose
#   BASTION_OCID                   # override terraform output (bastion_id)
#   INSTANCE_OCID                  # override the OKE node OCID (else node pool)
#   INSTANCE_PRIVATE_IP            # override the OKE node private IP
#   BASTION_TARGET_USER            # default: opc (Oracle Linux OKE nodes)
#   BASTION_SSH_KEY                # default: ~/.ssh/id_ed25519
#   OPENGATE_TERRAFORM_DIR         # default: deploy/terraform
#
# The SSH target is the OKE worker node (the compose VM it formerly fronted was
# decommissioned). The node's OCID + private IP are resolved live from the
# cluster node pool (oke_node_pool_id terraform output) via `oci ce node-pool get`.
#
# Prerequisites: oci CLI, jq, terraform (if not pre-setting the OCIDs).
# IAM: `manage bastion-session` on the compartment + `read instance` on the
# target. See docs/Infrastructure.md → "Operator access via OCI Bastion".

set -Eeuo pipefail

# ──────────────────────────────────────────────────────────────────────────
# Constants and basic globals
# ──────────────────────────────────────────────────────────────────────────

DEBUG="${OPENGATE_BASTION_DEBUG:-0}"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/opengate"
CACHE_FILE="$CACHE_DIR/bastion-session.json"
LOG_FILE="$CACHE_DIR/bastion-session.log"
LOG_MAX_BYTES=$(( 5 * 1024 * 1024 ))
TERRAFORM_DIR="${OPENGATE_TERRAFORM_DIR:-deploy/terraform}"
TARGET_USER="${BASTION_TARGET_USER:-opc}"
SSH_KEY="${BASTION_SSH_KEY:-$HOME/.ssh/id_ed25519}"
SSH_PUBKEY="${SSH_KEY}.pub"
# 5 minutes of headroom on the session TTL so a session about to expire
# is replaced proactively rather than mid-ssh.
TTL_HEADROOM_SECONDS=300
# Bastion's per-session TTL cap, in seconds. Pinned to the bastion's
# max-session-ttl-in-seconds (terraform) — set in modules/bastion/main.tf.
# Note: the OCI CLI flag is `--session-ttl` despite the API field being
# `session-ttl-in-seconds`; the names diverge.
SESSION_TTL_REQUEST=10800

mkdir -p "$CACHE_DIR"

# ──────────────────────────────────────────────────────────────────────────
# Logging and error handling
# ──────────────────────────────────────────────────────────────────────────

now_epoch()  { date -u +%s; }
iso_utc()    { date -u +%Y-%m-%dT%H:%M:%SZ; }

# Append to the persistent log. Rotates when LOG_MAX_BYTES is exceeded.
log_to_file() {
  if [[ -f "$LOG_FILE" ]]; then
    local size
    size=$(stat -c '%s' "$LOG_FILE" 2>/dev/null || echo 0)
    if (( size > LOG_MAX_BYTES )); then
      mv -f "$LOG_FILE" "${LOG_FILE}.1"
    fi
  fi
  printf '[%s] [pid=%s] %s\n' "$(iso_utc)" "$$" "$*" >>"$LOG_FILE"
}

log()   { printf '==> %s\n' "$*" >&2; log_to_file "INFO  $*"; }
warn()  { printf 'WARN: %s\n' "$*" >&2; log_to_file "WARN  $*"; }
err()   { printf 'ERROR: %s\n' "$*" >&2; log_to_file "ERROR $*"; exit 1; }
debug() { [[ "$DEBUG" == "1" ]] && printf '... %s\n' "$*" >&2; log_to_file "DEBUG $*"; return 0; }

# ERR trap: prints the failing line + command + exit code, points the
# operator at the persistent log. set -E (above) ensures functions inherit.
on_err() {
  local exit_code=$? lineno=${BASH_LINENO[0]:-0} cmd=${BASH_COMMAND:-?}
  log_to_file "FATAL exit=$exit_code line=$lineno cmd=[$cmd]"
  printf 'ERROR: bastion-session.sh failed at line %s (exit %s): %s\n' \
    "$lineno" "$exit_code" "$cmd" >&2
  printf '       full history: %s\n' "$LOG_FILE" >&2
  if [[ "$DEBUG" != "1" ]]; then
    printf '       re-run with OPENGATE_BASTION_DEBUG=1 for OCI CLI --debug + set -x\n' >&2
  fi
}
trap on_err ERR

if [[ "$DEBUG" == "1" ]]; then
  log_to_file "── invocation: DEBUG=1 args=[$*]"
  set -x
else
  log_to_file "── invocation: args=[$*]"
fi

# ──────────────────────────────────────────────────────────────────────────
# Subcommand dispatch (validated before doing any real work)
# ──────────────────────────────────────────────────────────────────────────

MODE="${1:-ssh}"
case "$MODE" in
  ssh|diagnose|purge) ;;
  *) err "unknown subcommand '$MODE' (expected: ssh | diagnose | purge)" ;;
esac

if [[ "$MODE" == "purge" ]]; then
  if [[ -f "$CACHE_FILE" ]]; then
    log "removing $CACHE_FILE"
    rm -f "$CACHE_FILE"
  else
    log "cache already empty ($CACHE_FILE not present)"
  fi
  exit 0
fi

# ──────────────────────────────────────────────────────────────────────────
# Prerequisite + input checks
# ──────────────────────────────────────────────────────────────────────────

command -v oci >/dev/null 2>&1 || err "oci CLI not found. Install: https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm"
command -v jq  >/dev/null 2>&1 || err "jq not found. Install: apt install jq | brew install jq"
[[ -f "$SSH_KEY"    ]] || err "SSH private key not found at $SSH_KEY (set BASTION_SSH_KEY to override)"
[[ -f "$SSH_PUBKEY" ]] || err "SSH public key not found at $SSH_PUBKEY (must sit next to the private key as .pub)"

# Resolve identifiers unless the caller pre-set them. BASTION_OCID and the node
# pool OCID come from terraform outputs; the SSH target is the OKE worker node,
# whose OCID + private IP are read live from the node pool (OKE owns node
# identity — there is no terraform output for them). `oci` is invoked directly
# here because the oci_cmd wrapper is defined further down the file.
if [[ -z "${BASTION_OCID:-}" || -z "${INSTANCE_OCID:-}" || -z "${INSTANCE_PRIVATE_IP:-}" ]]; then
  command -v terraform >/dev/null 2>&1 || err "terraform not found (and BASTION_OCID/INSTANCE_OCID/INSTANCE_PRIVATE_IP not pre-set)"
  [[ -d "$TERRAFORM_DIR" ]] || err "terraform dir not found at $TERRAFORM_DIR (set OPENGATE_TERRAFORM_DIR to override)"
  debug "resolving identifiers from $TERRAFORM_DIR + OKE node pool"
  BASTION_OCID="${BASTION_OCID:-$(terraform -chdir="$TERRAFORM_DIR" output -raw bastion_id 2>/dev/null || true)}"

  if [[ -z "${INSTANCE_OCID:-}" || -z "${INSTANCE_PRIVATE_IP:-}" ]]; then
    node_pool_id=$(terraform -chdir="$TERRAFORM_DIR" output -raw oke_node_pool_id 2>/dev/null || true)
    [[ -n "$node_pool_id" ]] || err "oke_node_pool_id is empty. Run 'terraform -chdir=$TERRAFORM_DIR apply' or pre-set INSTANCE_OCID + INSTANCE_PRIVATE_IP."
    debug "resolving worker node from node pool $node_pool_id"
    node_json=$(oci ce node-pool get --node-pool-id "$node_pool_id" --query 'data.nodes' 2>/dev/null) \
      || err "oci ce node-pool get failed — cannot resolve the worker node. Check IAM (read on the node pool) and the node pool id."
    INSTANCE_OCID="${INSTANCE_OCID:-$(jq -r 'map(select(."lifecycle-state"=="ACTIVE")) | first | .id // empty' <<<"$node_json")}"
    INSTANCE_PRIVATE_IP="${INSTANCE_PRIVATE_IP:-$(jq -r 'map(select(."lifecycle-state"=="ACTIVE")) | first | ."private-ip" // empty' <<<"$node_json")}"
  fi
fi

[[ -n "$BASTION_OCID"        ]] || err "bastion_id is empty. Run 'terraform -chdir=$TERRAFORM_DIR apply' or set BASTION_OCID."
[[ -n "$INSTANCE_OCID"       ]] || err "could not resolve an ACTIVE OKE worker-node OCID from the node pool. Set INSTANCE_OCID to override."
[[ -n "$INSTANCE_PRIVATE_IP" ]] || err "could not resolve the OKE worker-node private IP from the node pool. Set INSTANCE_PRIVATE_IP to override."

debug "BASTION_OCID=$BASTION_OCID"
debug "INSTANCE_OCID=$INSTANCE_OCID"
debug "INSTANCE_PRIVATE_IP=$INSTANCE_PRIVATE_IP"
debug "TARGET_USER=$TARGET_USER"
debug "SSH_KEY=$SSH_KEY"

# ──────────────────────────────────────────────────────────────────────────
# OCI CLI wrapper — single source of truth for invocation, capture, surfacing
# ──────────────────────────────────────────────────────────────────────────

# Runs `oci "$@"` and prints stdout on success. On failure: prints the
# captured stderr to OUR stderr AND the log file, then returns the OCI
# CLI's exit code unchanged. In DEBUG mode passes --debug to OCI.
#
# Usage:
#   if ! out=$(oci_cmd bastion bastion get --bastion-id "$id"); then
#     err "fetch failed"
#   fi
oci_cmd() {
  local stderr_file out rc
  stderr_file=$(mktemp)
  log_to_file "OCI $*"
  if [[ "$DEBUG" == "1" ]]; then
    if out=$(oci --debug "$@" 2>"$stderr_file"); then rc=0; else rc=$?; fi
  else
    if out=$(oci "$@" 2>"$stderr_file"); then rc=0; else rc=$?; fi
  fi
  if (( rc != 0 )); then
    local err_payload
    err_payload=$(cat "$stderr_file")
    log_to_file "OCI failed (rc=$rc): $err_payload"
    printf '%s\n' "$err_payload" >&2
  elif [[ "$DEBUG" == "1" ]]; then
    log_to_file "OCI stderr (rc=0): $(cat "$stderr_file")"
  fi
  rm -f "$stderr_file"
  printf '%s' "$out"
  return "$rc"
}

# ──────────────────────────────────────────────────────────────────────────
# Cache helpers
# ──────────────────────────────────────────────────────────────────────────

# Returns 0 if the cached session covers the current target AND has at
# least TTL_HEADROOM_SECONDS remaining.
cache_is_fresh() {
  [[ -f "$CACHE_FILE" ]] || { debug "cache miss: $CACHE_FILE absent"; return 1; }
  local cached_bastion cached_target cached_user expires_at session_id
  cached_bastion=$(jq -r '.bastion_id  // empty' "$CACHE_FILE")
  cached_target=$( jq -r '.target_ocid // empty' "$CACHE_FILE")
  cached_user=$(   jq -r '.target_user // empty' "$CACHE_FILE")
  expires_at=$(    jq -r '.expires_at  // 0'     "$CACHE_FILE")
  session_id=$(    jq -r '.session_id  // empty' "$CACHE_FILE")

  if [[ "$cached_bastion" != "$BASTION_OCID" ]]; then
    debug "cache miss: bastion changed (cached=$cached_bastion live=$BASTION_OCID)"
    return 1
  fi
  if [[ "$cached_target" != "$INSTANCE_OCID" ]]; then
    debug "cache miss: target changed (cached=$cached_target live=$INSTANCE_OCID)"
    return 1
  fi
  if [[ "$cached_user" != "$TARGET_USER" ]]; then
    debug "cache miss: target_user changed (cached=$cached_user live=$TARGET_USER)"
    return 1
  fi
  local now remaining
  now=$(now_epoch)
  remaining=$(( expires_at - now ))
  if (( remaining <= TTL_HEADROOM_SECONDS )); then
    debug "cache miss: session $session_id expires in ${remaining}s (<= ${TTL_HEADROOM_SECONDS}s headroom)"
    return 1
  fi
  debug "cache hit: session $session_id, ${remaining}s remaining"
  return 0
}

# ──────────────────────────────────────────────────────────────────────────
# Session create
# ──────────────────────────────────────────────────────────────────────────

# Look for an existing ACTIVE session matching (bastion, target instance,
# target user) with enough TTL remaining. Returns 0 + prints the OCID on
# stdout if found, else returns 1 silently. Lets the script self-heal
# from orphan sessions left behind by interrupted previous runs (Ctrl-C
# during create, killed `make tunnel`, etc.) — without this, every such
# orphan stays live for the full 3 h TTL while the script wastes another
# session quota slot on a duplicate create.
find_reusable_session() {
  local sessions_json match
  if ! sessions_json=$(oci_cmd bastion session list \
      --bastion-id "$BASTION_OCID" \
      --session-lifecycle-state ACTIVE \
      --all --query 'data'); then
    debug "session list failed; falling through to create"
    return 1
  fi
  # JMESPath isn't available here — use jq. Match on target instance OCID
  # + os-username + at-least-headroom seconds of TTL remaining.
  match=$(jq -r \
    --arg target "$INSTANCE_OCID" \
    --arg user   "$TARGET_USER" \
    --argjson headroom "$TTL_HEADROOM_SECONDS" \
    --argjson now "$(now_epoch)" '
      [ .[]
        | select(."target-resource-details"."target-resource-id" == $target)
        | select(."target-resource-details"."target-resource-operating-system-user-name" == $user)
        | select(
            (."time-created" | sub("\\.[0-9]+"; "") | sub("\\+00:00$"; "Z") | fromdateiso8601)
            + (."session-ttl-in-seconds" // 1800)
            - $now > $headroom
          )
        | .id
      ] | first // empty' <<<"$sessions_json")
  [[ -n "$match" ]] || return 1
  printf '%s' "$match"
}

create_session() {
  local session_name session_id session_json session_ttl ssh_proxy

  # First-chance: discover and reuse an orphan ACTIVE session matching our
  # target. Avoids consuming a quota slot on a duplicate after an
  # interrupted previous run. Only the cache write happens regardless.
  if session_id=$(find_reusable_session) && [[ -n "$session_id" ]]; then
    log "Reusing pre-existing ACTIVE session $session_id (orphan from a prior interrupted run)"
  else
    session_name="opengate-$(date -u +%Y%m%d-%H%M%S)"
    log "Creating Managed SSH session via bastion $BASTION_OCID (TTL ${SESSION_TTL_REQUEST}s) ..."
    # `--wait-for-state SUCCEEDED` returns the work-request payload, which
    # does NOT carry `ssh-metadata` reliably. Capture the session OCID
    # here; the next step re-fetches the canonical shape via `session get`.
    if ! session_id=$(oci_cmd bastion session create-managed-ssh \
      --bastion-id "$BASTION_OCID" \
      --target-resource-id "$INSTANCE_OCID" \
      --target-os-username "$TARGET_USER" \
      --target-private-ip "$INSTANCE_PRIVATE_IP" \
      --display-name "$session_name" \
      --ssh-public-key-file "$SSH_PUBKEY" \
      --session-ttl "$SESSION_TTL_REQUEST" \
      --wait-for-state SUCCEEDED \
      --query 'data.id' --raw-output); then
      err "session create failed. See the OCI error above. Common causes: IAM ('manage bastion-session' on compartment + 'read instance' on target), Cloud Agent Bastion plugin not RUNNING on the VM, bastion's client_cidr_block_allow_list rejecting your IP, or the bastion's active-session quota."
    fi
    [[ -n "$session_id" ]] || err "session create returned empty session id"
    log "Session created: $session_id"
  fi

  if ! session_json=$(oci_cmd bastion session get \
    --session-id "$session_id" \
    --query 'data'); then
    err "session was created ($session_id) but session get failed. The session is live but ssh-metadata is unreachable; cache will not be written."
  fi

  ssh_proxy=$(jq -r '."ssh-metadata".command // empty' <<<"$session_json")
  [[ -n "$ssh_proxy" ]] || err "session $session_id has no ssh-metadata.command — OCI API surface drift?"

  # Read the actual TTL OCI granted (vs assuming SESSION_TTL_REQUEST) so the
  # cache expiry stays honest if a future bastion policy clamps it lower.
  session_ttl=$(jq -r '."session-ttl-in-seconds" // 1800' <<<"$session_json")
  debug "OCI granted TTL=${session_ttl}s (requested ${SESSION_TTL_REQUEST}s)"

  local now
  now=$(now_epoch)
  jq -n \
    --arg bastion_id   "$BASTION_OCID" \
    --arg target_ocid  "$INSTANCE_OCID" \
    --arg target_user  "$TARGET_USER" \
    --arg target_ip    "$INSTANCE_PRIVATE_IP" \
    --arg session_id   "$session_id" \
    --arg ssh_command  "$ssh_proxy" \
    --argjson created_at "$now" \
    --argjson expires_at "$(( now + session_ttl ))" \
    '{$bastion_id, $target_ocid, $target_user, $target_ip, $session_id, $ssh_command, $created_at, $expires_at}' \
    > "$CACHE_FILE"
  chmod 600 "$CACHE_FILE"
  log "cache written ($CACHE_FILE). Refresh in $(( session_ttl - TTL_HEADROOM_SECONDS ))s."
}

# ──────────────────────────────────────────────────────────────────────────
# diagnose subcommand — read-only sanity checks
# ──────────────────────────────────────────────────────────────────────────

cmd_diagnose() {
  # Capture each tool's full output first, then take the first line via
  # parameter expansion. The naive `cmd | head -1` pattern combined with
  # `set -o pipefail` raises a false "command failed" when the producer
  # gets SIGPIPE after head exits (any multi-line output is affected),
  # which made one of the prerequisite lines appear as "(not found)"
  # even when the tool was installed. Sourcing first, slicing second.
  local oci_v jq_v tf_v ssh_v
  oci_v=$(oci --version 2>&1)         || oci_v="(not found)"
  jq_v=$(jq --version 2>&1)           || jq_v="(not found)"
  tf_v=$(terraform version 2>/dev/null) || tf_v="(not found)"
  ssh_v=$(ssh -V 2>&1)                || ssh_v="(not found)"

  log "── prerequisites"
  printf '  oci:       %s\n' "${oci_v%%$'\n'*}"
  printf '  jq:        %s\n' "${jq_v%%$'\n'*}"
  printf '  terraform: %s\n' "${tf_v%%$'\n'*}"
  printf '  ssh:       %s\n' "${ssh_v%%$'\n'*}"

  log "── inputs"
  printf '  BASTION_OCID:        %s\n' "$BASTION_OCID"
  printf '  INSTANCE_OCID:       %s\n' "$INSTANCE_OCID"
  printf '  INSTANCE_PRIVATE_IP: %s\n' "$INSTANCE_PRIVATE_IP"
  printf '  TARGET_USER:         %s\n' "$TARGET_USER"
  printf '  SSH_KEY:             %s\n' "$SSH_KEY"

  log "── bastion state"
  local bastion_json
  if bastion_json=$(oci_cmd bastion bastion get --bastion-id "$BASTION_OCID" --query 'data'); then
    jq '{name, "lifecycle-state", "max-session-ttl-in-seconds", "client-cidr-block-allow-list"}' <<<"$bastion_json"
  else
    warn "bastion get failed"
  fi

  log "── active sessions on this bastion (note: OCI caps at 10 concurrent)"
  local sessions_json
  if sessions_json=$(oci_cmd bastion session list \
      --bastion-id "$BASTION_OCID" \
      --session-lifecycle-state ACTIVE \
      --all --query 'data'); then
    jq '[.[] | {id, "display-name", "session-ttl-in-seconds", "time-created"}]' <<<"$sessions_json"
  else
    warn "session list failed"
  fi

  log "── Cloud Agent Bastion plugin status on target instance"
  local compartment_id
  if compartment_id=$(oci_cmd compute instance get \
      --instance-id "$INSTANCE_OCID" \
      --query 'data."compartment-id"' --raw-output 2>/dev/null); then
    # JMESPath expression — single quotes are intentional (no shell expansion).
    # shellcheck disable=SC2016
    if ! oci_cmd instance-agent plugin list \
        --instanceagent-id "$INSTANCE_OCID" \
        --compartment-id "$compartment_id" \
        --query 'data[?name==`"Bastion"`].{name:name,status:status}'; then
      warn "plugin list failed"
    fi
  else
    warn "compute instance get failed — cannot determine compartment for plugin lookup"
  fi

  log "── local cache"
  if [[ -f "$CACHE_FILE" ]]; then
    jq '. + {expires_in_min: ((.expires_at - now) / 60 | round)}' "$CACHE_FILE"
  else
    printf '  (no cache file at %s)\n' "$CACHE_FILE"
  fi

  log "── log file: $LOG_FILE ($(wc -l <"$LOG_FILE" 2>/dev/null || echo 0) lines)"
}

# ──────────────────────────────────────────────────────────────────────────
# Main flow
# ──────────────────────────────────────────────────────────────────────────

if [[ "$MODE" == "diagnose" ]]; then
  cmd_diagnose
  exit 0
fi

if cache_is_fresh; then
  log "Reusing cached session ($(jq -r .session_id "$CACHE_FILE"))"
else
  create_session
fi

# OCI emits a ProxyCommand using `<privateKey>` as a placeholder path; the
# operator's key location is operator-side state, so patch the `-i` flags
# (both the outer ssh and the inner ProxyCommand ssh) to point at $SSH_KEY.
raw_command=$(jq -r '.ssh_command' "$CACHE_FILE")
patched_command=$(echo "$raw_command" | sed -E "s|-i [^ \"]+|-i $SSH_KEY|g")
debug "patched ssh command: $patched_command"

case "$MODE" in
  ssh)
    log "Opening interactive shell on $TARGET_USER@$INSTANCE_PRIVATE_IP (OKE worker node)"
    exec bash -c "$patched_command"
    ;;
esac
