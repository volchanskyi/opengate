#!/usr/bin/env bash
# Creates or reuses a Managed SSH session through the OCI Bastion to the OKE worker node.
# The session OCID, ssh command and expiry are cached in ~/.cache/opengate/bastion-session.json.

# Every run appends to ~/.cache/opengate/bastion-session.log, trimmed at about 5 MB.

# Usage:
#   bastion-session.sh ssh        interactive shell on the OKE worker node
#   bastion-session.sh diagnose   read-only checks of bastion, plugin, sessions and cache
#   bastion-session.sh purge      delete the local cache file

# Environment:
#   OPENGATE_BASTION_DEBUG   1 enables set -x and OCI CLI --debug
#   BASTION_OCID             overrides the bastion_id terraform output
#   INSTANCE_OCID            overrides the OKE node OCID, otherwise read from the node pool
#   INSTANCE_PRIVATE_IP      overrides the OKE node private IP
#   BASTION_TARGET_USER      defaults to opc
#   BASTION_SSH_KEY          defaults to ~/.ssh/id_ed25519
#   OPENGATE_TERRAFORM_DIR   defaults to deploy/terraform

# The node OCID and private IP resolve live from the node pool via `oci ce node-pool get`.
# Requires the oci CLI, jq and terraform; IAM needs `manage bastion-session` and `read instance`.

set -Eeuo pipefail

DEBUG="${OPENGATE_BASTION_DEBUG:-0}"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/opengate"
CACHE_FILE="$CACHE_DIR/bastion-session.json"
LOG_FILE="$CACHE_DIR/bastion-session.log"
LOG_MAX_BYTES=$((5 * 1024 * 1024))
TERRAFORM_DIR="${OPENGATE_TERRAFORM_DIR:-deploy/terraform}"
TARGET_USER="${BASTION_TARGET_USER:-opc}"
SSH_KEY="${BASTION_SSH_KEY:-$HOME/.ssh/id_ed25519}"
SSH_PUBKEY="${SSH_KEY}.pub"
# Five minutes of headroom replaces a session about to expire before an ssh starts.
TTL_HEADROOM_SECONDS=300
# The bastion's per-session TTL cap in seconds, set by max-session-ttl-in-seconds in terraform.
SESSION_TTL_REQUEST=10800

mkdir -p "$CACHE_DIR"
# The cache holds the session OCID and the log records both OCIDs plus the
# worker node's private IP, so neither is readable by other local accounts.
chmod 700 "$CACHE_DIR"
[[ -f "$LOG_FILE" ]] || : >"$LOG_FILE"
chmod 600 "$LOG_FILE"

now_epoch() { date -u +%s; }
iso_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

log_to_file() {
  if [[ -f "$LOG_FILE" ]]; then
    local size
    size=$(stat -c '%s' "$LOG_FILE" 2>/dev/null || echo 0)
    if ((size > LOG_MAX_BYTES)); then
      mv -f "$LOG_FILE" "${LOG_FILE}.1"
    fi
  fi
  printf '[%s] [pid=%s] %s\n' "$(iso_utc)" "$$" "$*" >>"$LOG_FILE"
}

log() {
  printf '==> %s\n' "$*" >&2
  log_to_file "INFO  $*"
}
warn() {
  printf 'WARN: %s\n' "$*" >&2
  log_to_file "WARN  $*"
}
err() {
  printf 'ERROR: %s\n' "$*" >&2
  log_to_file "ERROR $*"
  exit 1
}
debug() {
  [[ "$DEBUG" == "1" ]] && printf '... %s\n' "$*" >&2
  log_to_file "DEBUG $*"
  return 0
}

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

MODE="${1:-ssh}"
case "$MODE" in
  ssh | diagnose | purge) ;;
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

command -v oci >/dev/null 2>&1 || err "oci CLI not found. Install: https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm"
command -v jq >/dev/null 2>&1 || err "jq not found. Install: apt install jq | brew install jq"
[[ -f "$SSH_KEY" ]] || err "SSH private key not found at $SSH_KEY (set BASTION_SSH_KEY to override)"
[[ -f "$SSH_PUBKEY" ]] || err "SSH public key not found at $SSH_PUBKEY (must sit next to the private key as .pub)"

# The bastion and node pool OCIDs come from terraform outputs; the node OCID and private IP are
# read from the node pool with `oci` directly, since oci_cmd is defined further down.
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

[[ -n "$BASTION_OCID" ]] || err "bastion_id is empty. Run 'terraform -chdir=$TERRAFORM_DIR apply' or set BASTION_OCID."
[[ -n "$INSTANCE_OCID" ]] || err "could not resolve an ACTIVE OKE worker-node OCID from the node pool. Set INSTANCE_OCID to override."
[[ -n "$INSTANCE_PRIVATE_IP" ]] || err "could not resolve the OKE worker-node private IP from the node pool. Set INSTANCE_PRIVATE_IP to override."

debug "BASTION_OCID=$BASTION_OCID"
debug "INSTANCE_OCID=$INSTANCE_OCID"
debug "INSTANCE_PRIVATE_IP=$INSTANCE_PRIVATE_IP"
debug "TARGET_USER=$TARGET_USER"
debug "SSH_KEY=$SSH_KEY"

# Runs `oci "$@"` and prints stdout on success; on failure copies stderr to the log and
# returns the OCI exit code unchanged. DEBUG mode passes --debug.
oci_cmd() {
  local stderr_file out rc
  stderr_file=$(mktemp)
  log_to_file "OCI $*"
  if [[ "$DEBUG" == "1" ]]; then
    if out=$(oci --debug "$@" 2>"$stderr_file"); then rc=0; else rc=$?; fi
  else
    if out=$(oci "$@" 2>"$stderr_file"); then rc=0; else rc=$?; fi
  fi
  if ((rc != 0)); then
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

# Returns 0 if the cached session covers the current target AND has at
# least TTL_HEADROOM_SECONDS remaining.
cache_is_fresh() {
  [[ -f "$CACHE_FILE" ]] || {
    debug "cache miss: $CACHE_FILE absent"
    return 1
  }
  local cached_bastion cached_target cached_user expires_at session_id
  cached_bastion=$(jq -r '.bastion_id  // empty' "$CACHE_FILE")
  cached_target=$(jq -r '.target_ocid // empty' "$CACHE_FILE")
  cached_user=$(jq -r '.target_user // empty' "$CACHE_FILE")
  expires_at=$(jq -r '.expires_at  // 0' "$CACHE_FILE")
  session_id=$(jq -r '.session_id  // empty' "$CACHE_FILE")

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
  remaining=$((expires_at - now))
  if ((remaining <= TTL_HEADROOM_SECONDS)); then
    debug "cache miss: session $session_id expires in ${remaining}s (<= ${TTL_HEADROOM_SECONDS}s headroom)"
    return 1
  fi
  debug "cache hit: session $session_id, ${remaining}s remaining"
  return 0
}

# Finds an ACTIVE session for this bastion, instance and user with enough TTL left, so orphans
# of interrupted runs are reused; prints its OCID and returns 0, else returns 1 silently.
find_reusable_session() {
  local sessions_json match
  if ! sessions_json=$(oci_cmd bastion session list \
    --bastion-id "$BASTION_OCID" \
    --session-lifecycle-state ACTIVE \
    --all --query 'data'); then
    debug "session list failed; falling through to create"
    return 1
  fi
  # jq matches the target instance, os-username and at least the headroom seconds of TTL left.
  match=$(jq -r \
    --arg target "$INSTANCE_OCID" \
    --arg user "$TARGET_USER" \
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
  local session_created=false

  # Reuses an orphan ACTIVE session for this target, saving a quota slot after an interrupted run.
  if session_id=$(find_reusable_session) && [[ -n "$session_id" ]]; then
    log "Reusing pre-existing ACTIVE session $session_id (orphan from a prior interrupted run)"
  else
    session_name="opengate-$(date -u +%Y%m%d-%H%M%S)"
    log "Creating Managed SSH session via bastion $BASTION_OCID (TTL ${SESSION_TTL_REQUEST}s) ..."
    # --wait-for-state returns a work-request payload without reliable ssh-metadata, so the
    # session OCID is captured here and `session get` re-fetches the full record.
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
    session_created=true
    log "Session created: $session_id"
  fi

  if ! session_json=$(oci_cmd bastion session get \
    --session-id "$session_id" \
    --query 'data'); then
    if [[ "$session_created" == "true" ]]; then
      warn "session metadata fetch failed; deleting newly-created session $session_id"
      oci_cmd bastion session delete --session-id "$session_id" --force >/dev/null || true
    fi
    err "session was created ($session_id) but session get failed. The session is live but ssh-metadata is unreachable; cache will not be written."
  fi

  ssh_proxy=$(jq -r '."ssh-metadata".command // empty' <<<"$session_json")
  [[ -n "$ssh_proxy" ]] || err "session $session_id has no ssh-metadata.command — OCI API surface drift?"

  # Reads the TTL OCI granted so the cache expiry stays honest if the bastion clamps it.
  session_ttl=$(jq -r '."session-ttl-in-seconds" // 1800' <<<"$session_json")
  debug "OCI granted TTL=${session_ttl}s (requested ${SESSION_TTL_REQUEST}s)"

  local now
  now=$(now_epoch)
  jq -n \
    --arg bastion_id "$BASTION_OCID" \
    --arg target_ocid "$INSTANCE_OCID" \
    --arg target_user "$TARGET_USER" \
    --arg target_ip "$INSTANCE_PRIVATE_IP" \
    --arg session_id "$session_id" \
    --arg ssh_command "$ssh_proxy" \
    --argjson created_at "$now" \
    --argjson expires_at "$((now + session_ttl))" \
    '{$bastion_id, $target_ocid, $target_user, $target_ip, $session_id, $ssh_command, $created_at, $expires_at}' \
    >"$CACHE_FILE"
  chmod 600 "$CACHE_FILE"
  log "cache written ($CACHE_FILE). Refresh in $((session_ttl - TTL_HEADROOM_SECONDS))s."
}

cmd_diagnose() {
  # Output is captured before slicing; piping into head under pipefail fails on SIGPIPE.
  local oci_v jq_v tf_v ssh_v
  oci_v=$(oci --version 2>&1) || oci_v="(not found)"
  jq_v=$(jq --version 2>&1) || jq_v="(not found)"
  tf_v=$(terraform version 2>/dev/null) || tf_v="(not found)"
  ssh_v=$(ssh -V 2>&1) || ssh_v="(not found)"

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

if [[ "$MODE" == "diagnose" ]]; then
  cmd_diagnose
  exit 0
fi

if cache_is_fresh; then
  log "Reusing cached session ($(jq -r .session_id "$CACHE_FILE"))"
else
  create_session
fi

# OCI emits `<privateKey>` as the -i path; both ssh invocations are patched to use $SSH_KEY.
raw_command=$(jq -r '.ssh_command' "$CACHE_FILE")
patched_command=$(echo "$raw_command" | sed -E "s|-i [^ \"]+|-i $SSH_KEY|g")
debug "patched ssh command: $patched_command"

case "$MODE" in
  ssh)
    log "Opening interactive shell on $TARGET_USER@$INSTANCE_PRIVATE_IP (OKE worker node)"
    exec bash -c "$patched_command"
    ;;
esac
