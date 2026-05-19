#!/usr/bin/env bash
# OCI Bastion session wrapper. Creates (or reuses) a Managed SSH session and
# either opens an interactive shell (`ssh`) or forwards Grafana :3000 +
# Uptime Kuma :3001 (`tunnel`).
#
# Invoked via the root Makefile's `make ssh` / `make tunnel` targets.
#
# Caching: the active session OCID + expiry are persisted at
# ~/.cache/opengate/bastion-session.json. Subsequent invocations within the
# session TTL (3h, OCI cap) skip the 5–10 s session-create round trip.
#
# Inputs (resolved automatically from `terraform output` if unset):
#   BASTION_OCID         — OCI Bastion OCID (terraform output `bastion_id`)
#   INSTANCE_OCID        — Target VM OCID    (terraform output `instance_id`)
#   INSTANCE_PRIVATE_IP  — Target VM IP      (terraform output `instance_private_ip`)
#   BASTION_TARGET_USER  — defaults to ubuntu (the cloud-init user)
#   BASTION_SSH_KEY      — defaults to ~/.ssh/id_ed25519
#   BASTION_REGION       — defaults to us-sanjose-1
#
# Usage:
#   bastion-session.sh tunnel   # background-friendly: SSH + -L 3000 -L 3001
#   bastion-session.sh ssh      # interactive shell on the VM
#
# Prerequisites: oci CLI, jq, terraform. ~/.oci/config profile + an OCI IAM
# user with `manage bastion-session` on the compartment + `read instance` on
# the target VM. See docs/Infrastructure.md → "Operator access via OCI Bastion".

set -euo pipefail

MODE="${1:-tunnel}"
case "$MODE" in
  tunnel|ssh) ;;
  *) echo "ERROR: unknown mode '$MODE' (expected: tunnel | ssh)" >&2; exit 2 ;;
esac

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/opengate"
CACHE_FILE="$CACHE_DIR/bastion-session.json"
TERRAFORM_DIR="${OPENGATE_TERRAFORM_DIR:-deploy/terraform}"
TARGET_USER="${BASTION_TARGET_USER:-ubuntu}"
SSH_KEY="${BASTION_SSH_KEY:-$HOME/.ssh/id_ed25519}"
SSH_PUBKEY="${SSH_KEY}.pub"
# 5 minutes of headroom on the 3h TTL so a session that's about to expire
# is replaced proactively rather than mid-`ssh`.
TTL_HEADROOM_SECONDS=300

err() { echo "ERROR: $*" >&2; exit 1; }
log() { echo "==> $*" >&2; }

command -v oci  >/dev/null 2>&1 || err "oci CLI not found. Install: https://docs.oracle.com/iaas/Content/API/SDKDocs/cliinstall.htm"
command -v jq   >/dev/null 2>&1 || err "jq not found. Install: apt install jq | brew install jq"
[[ -f "$SSH_KEY"    ]] || err "SSH private key not found at $SSH_KEY (set BASTION_SSH_KEY to override)"
[[ -f "$SSH_PUBKEY" ]] || err "SSH public key not found at $SSH_PUBKEY (must sit next to the private key as .pub)"

# Resolve identifiers from terraform output unless caller pre-set them via env.
if [[ -z "${BASTION_OCID:-}" || -z "${INSTANCE_OCID:-}" || -z "${INSTANCE_PRIVATE_IP:-}" ]]; then
  command -v terraform >/dev/null 2>&1 || err "terraform not found (and BASTION_OCID/INSTANCE_OCID/INSTANCE_PRIVATE_IP not pre-set)"
  [[ -d "$TERRAFORM_DIR" ]] || err "terraform dir not found at $TERRAFORM_DIR (set OPENGATE_TERRAFORM_DIR to override)"

  BASTION_OCID="${BASTION_OCID:-$(terraform -chdir="$TERRAFORM_DIR" output -raw bastion_id 2>/dev/null || true)}"
  INSTANCE_OCID="${INSTANCE_OCID:-$(terraform -chdir="$TERRAFORM_DIR" output -raw instance_id 2>/dev/null || true)}"
  INSTANCE_PRIVATE_IP="${INSTANCE_PRIVATE_IP:-$(terraform -chdir="$TERRAFORM_DIR" output -raw instance_private_ip 2>/dev/null || true)}"

  [[ -n "$BASTION_OCID"        ]] || err "bastion_id not in terraform outputs. Run 'terraform -chdir=$TERRAFORM_DIR apply' first."
  [[ -n "$INSTANCE_OCID"       ]] || err "instance_id not in terraform outputs. Run 'terraform -chdir=$TERRAFORM_DIR apply' first."
  [[ -n "$INSTANCE_PRIVATE_IP" ]] || err "instance_private_ip not in terraform outputs. Run 'terraform -chdir=$TERRAFORM_DIR apply' first."
fi

mkdir -p "$CACHE_DIR"

now_epoch() { date -u +%s; }

# Returns 0 (true) if the cached session covers (bastion_id, target_ocid,
# target_user) AND has at least TTL_HEADROOM_SECONDS left, else 1.
cache_is_fresh() {
  [[ -f "$CACHE_FILE" ]] || return 1

  local cached_bastion cached_target cached_user expires_at
  cached_bastion=$(jq -r '.bastion_id    // empty' "$CACHE_FILE")
  cached_target=$( jq -r '.target_ocid   // empty' "$CACHE_FILE")
  cached_user=$(   jq -r '.target_user   // empty' "$CACHE_FILE")
  expires_at=$(    jq -r '.expires_at    // 0'      "$CACHE_FILE")

  [[ "$cached_bastion" == "$BASTION_OCID" ]] || return 1
  [[ "$cached_target"  == "$INSTANCE_OCID" ]] || return 1
  [[ "$cached_user"    == "$TARGET_USER" ]] || return 1
  (( expires_at > $(now_epoch) + TTL_HEADROOM_SECONDS ))
}

create_session() {
  local session_name session_json session_id ssh_proxy

  log "Creating Managed SSH session via bastion $BASTION_OCID ..."
  session_name="opengate-$(date -u +%Y%m%d-%H%M%S)"

  session_json=$(oci bastion session create-managed-ssh \
    --bastion-id "$BASTION_OCID" \
    --target-resource-id "$INSTANCE_OCID" \
    --target-os-username "$TARGET_USER" \
    --target-private-ip "$INSTANCE_PRIVATE_IP" \
    --display-name "$session_name" \
    --ssh-public-key-file "$SSH_PUBKEY" \
    --wait-for-state SUCCEEDED \
    --query 'data' 2>/dev/null) || err "oci bastion session create-managed-ssh failed. Check IAM: 'manage bastion-session' on the compartment, 'read instance' on the target."

  session_id=$(jq -r '.id // empty' <<<"$session_json")
  [[ -n "$session_id" ]] || err "session create returned no id"

  # OCI Bastion's ssh-metadata.command field includes the canonical
  # ProxyCommand line OCI expects clients to use; persist the raw command so
  # the wrapper does not have to reconstruct the bastion-host fqdn.
  ssh_proxy=$(jq -r '."ssh-metadata".command // empty' <<<"$session_json")
  [[ -n "$ssh_proxy" ]] || err "session metadata missing ssh-metadata.command — OCI API surface drift?"

  jq -n \
    --arg bastion_id   "$BASTION_OCID" \
    --arg target_ocid  "$INSTANCE_OCID" \
    --arg target_user  "$TARGET_USER" \
    --arg target_ip    "$INSTANCE_PRIVATE_IP" \
    --arg session_id   "$session_id" \
    --arg ssh_command  "$ssh_proxy" \
    --argjson created_at "$(now_epoch)" \
    --argjson expires_at "$(( $(now_epoch) + 10800 ))" \
    '{$bastion_id, $target_ocid, $target_user, $target_ip, $session_id, $ssh_command, $created_at, $expires_at}' \
    > "$CACHE_FILE"
  chmod 600 "$CACHE_FILE"

  log "Session $session_id created (TTL 10800s, refresh in $(( 10800 - TTL_HEADROOM_SECONDS ))s)."
}

if cache_is_fresh; then
  log "Reusing cached bastion session ($(jq -r .session_id "$CACHE_FILE"))."
else
  create_session
fi

# Extract the OCI-emitted ProxyCommand and inject our SSH private key into it
# (the OCI default uses ~/.ssh/id_rsa). The command shape is:
#   ssh -i <key> -o ProxyCommand="ssh -i <key> -W %h:%p -p 22 ocid1...@host.bastion.<region>.oci.oraclecloud.com" -p 22 <user>@<ip>
# We replace any `-i <path>` occurrence with our key so the wrapper works
# regardless of the operator's default key naming.
raw_command=$(jq -r '.ssh_command' "$CACHE_FILE")
# Normalize the -i argument to point at $SSH_KEY (the outer one AND the
# ProxyCommand's inner one). Use sed to replace the literal `-i <token>` pairs.
patched_command=$(echo "$raw_command" | sed -E "s|-i [^ \"]+|-i $SSH_KEY|g")

if [[ "$MODE" == "tunnel" ]]; then
  log "Opening Grafana (http://localhost:3000) and Uptime Kuma (http://localhost:3001) tunnels ..."
  # Append the -L forwards before the user@host tail. Splice via shell expansion
  # so the OCI-emitted quoting on ProxyCommand is preserved verbatim.
  exec bash -c "$patched_command -L 3000:localhost:3000 -L 3001:localhost:3001"
else
  log "Opening interactive shell ..."
  exec bash -c "$patched_command"
fi
