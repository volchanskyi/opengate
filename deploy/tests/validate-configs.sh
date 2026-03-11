#!/usr/bin/env bash
# Cross-config consistency tests for deploy/.
# Validates that ports, env vars, and Terraform variables stay in sync
# across docker-compose, OCI security list, UFW rules, and .env.example.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ERRORS=0

fail() {
  echo "FAIL: $1" >&2
  ERRORS=$((ERRORS + 1))
}

pass() {
  echo "  ok: $1"
}

# check_ports PROTO COMPOSE_PORTS UFW_PORTS OCI_PORTS
# Verifies every compose port appears in both UFW and OCI security list.
check_ports() {
  local proto="$1" compose_ports="$2" ufw_ports="$3" oci_ports="$4"
  for PORT in $compose_ports; do
    if ! echo "$ufw_ports" | grep -qx "$PORT"; then
      fail "$proto port $PORT in docker-compose.yml but missing from cloud-init.yaml UFW rules"
    else
      pass "$proto port $PORT in UFW"
    fi
    if ! echo "$oci_ports" | grep -qx "$PORT"; then
      fail "$proto port $PORT in docker-compose.yml but missing from OCI security list (main.tf)"
    else
      pass "$proto port $PORT in OCI security list"
    fi
  done
}

echo "=== Port consistency: docker-compose ↔ OCI security list ↔ UFW ==="

# Extract host ports from docker-compose.yml port mappings (format: "HOST:CONTAINER" or "HOST:CONTAINER/proto")
COMPOSE_TCP_PORTS=$(grep -oP '^\s*- "(\d+):\d+"' "$SCRIPT_DIR/docker-compose.yml" | grep -oP '"\K\d+' | sort -u)
COMPOSE_UDP_PORTS=$(grep -oP '^\s*- "(\d+):\d+/udp"' "$SCRIPT_DIR/docker-compose.yml" | grep -oP '"\K\d+' | sort -u)

# Extract ports from cloud-init UFW rules
UFW_TCP_PORTS=$(grep -oP 'ufw allow (\d+)/tcp' "$SCRIPT_DIR/terraform/cloud-init.yaml" | grep -oP '\d+' | sort -u)
UFW_UDP_PORTS=$(grep -oP 'ufw allow (\d+)/udp' "$SCRIPT_DIR/terraform/cloud-init.yaml" | grep -oP '\d+' | sort -u)

# Extract ports from OCI security list ingress rules (protocol "6" = TCP, "17" = UDP)
OCI_TCP_PORTS=$(grep -A3 'protocol.*=.*"6"' "$SCRIPT_DIR/terraform/main.tf" | grep -oP 'min\s*=\s*\K\d+' | sort -u)
OCI_UDP_PORTS=$(grep -A3 'protocol.*=.*"17"' "$SCRIPT_DIR/terraform/main.tf" | grep -oP 'min\s*=\s*\K\d+' | sort -u)

check_ports "TCP" "$COMPOSE_TCP_PORTS" "$UFW_TCP_PORTS" "$OCI_TCP_PORTS"
check_ports "UDP" "$COMPOSE_UDP_PORTS" "$UFW_UDP_PORTS" "$OCI_UDP_PORTS"

echo ""
echo "=== Env var coverage: docker-compose ↔ .env.example ==="

# Extract ${VAR...} references from docker-compose.yml (strip :? and :- defaults)
COMPOSE_VARS=$(grep -oP '\$\{(\w+)' "$SCRIPT_DIR/docker-compose.yml" | sed 's/\${//' | sort -u)

# Extract variable names from .env.example (lines like KEY=value, ignoring comments)
ENV_VARS=$(grep -oP '^\w+(?==)' "$SCRIPT_DIR/.env.example" | sort -u)

for VAR in $COMPOSE_VARS; do
  if ! echo "$ENV_VARS" | grep -qx "$VAR"; then
    fail "Variable \${$VAR} in docker-compose.yml but missing from .env.example"
  else
    pass "\${$VAR} documented in .env.example"
  fi
done

echo ""
echo "=== Tfvars completeness: required variables ↔ terraform.tfvars.example ==="

# Extract variable names that have no default (required variables)
# Pattern: variable "name" { ... } blocks WITHOUT a "default" line
REQUIRED_VARS=$(awk '
  /^variable "/ { name=$2; gsub(/"/, "", name); has_default=0 }
  /default[[:space:]]*=/ { has_default=1 }
  /^}/ { if (!has_default && name != "") print name; name="" }
' "$SCRIPT_DIR/terraform/variables.tf" | sort)

# Extract variable names from terraform.tfvars.example
TFVARS_VARS=$(grep -oP '^\w+(?=\s*=)' "$SCRIPT_DIR/terraform/terraform.tfvars.example" | sort -u)

for VAR in $REQUIRED_VARS; do
  if ! echo "$TFVARS_VARS" | grep -qx "$VAR"; then
    fail "Required Terraform variable \"$VAR\" (no default) missing from terraform.tfvars.example"
  else
    pass "Terraform variable \"$VAR\" in tfvars.example"
  fi
done

echo ""
echo "=== Cloud-init header ==="

# The #cloud-config magic header must appear in the first 2 lines
# (a yamllint directive comment may precede it)
if head -2 "$SCRIPT_DIR/terraform/cloud-init.yaml" | grep -qx "#cloud-config"; then
  pass "cloud-init.yaml contains #cloud-config header"
else
  fail "cloud-init.yaml must contain '#cloud-config' in its first 2 lines"
fi

echo ""
if [ "$ERRORS" -gt 0 ]; then
  echo "FAILED: $ERRORS error(s) found"
  exit 1
else
  echo "ALL PASSED"
  exit 0
fi
