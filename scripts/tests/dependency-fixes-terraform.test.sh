#!/usr/bin/env bash
# Tests the Terraform reader of scripts/dependency-fixes: the CLI at its pin and at the newest of
# the line the workflows float on, each provider and tflint plugin, and the Kubernetes version.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PACKAGE="$ROOT/scripts/dependency-fixes"
FAKES="$SCRIPT_DIR/fixtures/dependency-fixes"

PASS=0
FAIL=0
FAILURES=()
pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}
fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
REPO="$WORK/repo"
HTTP="$WORK/http"
VULNS="$HTTP/api.osv.dev/v1/vulns"
PROXY="$HTTP/proxy.golang.org/github.com"
CLI="$PROXY/hashicorp/terraform/@v"
PLUGIN="$PROXY/terraform-linters/tflint-ruleset-terraform/@v"
RAW="$HTTP/raw.githubusercontent.com/oracle/terraform-provider-oci"
mkdir -p "$REPO/scripts/lib" "$REPO/deploy/terraform" "$REPO/.github/workflows" "$WORK/bin" "$VULNS" \
  "$CLI" "$PLUGIN" "$RAW/v6.1.0" "$RAW/v6.1.2" "$RAW/v7.0.0" "$HTTP/registry.terraform.io/v1/providers/oracle/oci"
ln -s "$PACKAGE" "$REPO/scripts/dependency-fixes"
ln -s "$FAKES/fake-curl.py" "$WORK/bin/curl"
printf 'export TOOL_VERSION_DEMO="1.0.0"\nexport TOOL_SOURCE_DEMO="Go:example.test/demo"\n' \
  >"$REPO/scripts/lib/tool-versions.sh"
echo "1.9.5" >"$REPO/deploy/terraform/.terraform-version"
printf 'jobs:\n  plan:\n    steps:\n      - with:\n          terraform_version: 1.9.x\n' \
  >"$REPO/.github/workflows/ci.yml"
cat >"$REPO/deploy/terraform/.terraform.lock.hcl" <<'LOCK'
provider "registry.terraform.io/oracle/oci" {
  version     = "6.1.0"
  constraints = "~> 6.1"
  hashes = [
    "h1:AAAA=",
  ]
}
LOCK
cat >"$REPO/deploy/terraform/.tflint.hcl" <<'TFLINT'
plugin "terraform" {
  enabled = true
  version = "0.10.0"
  source  = "github.com/terraform-linters/tflint-ruleset-terraform"
}
TFLINT
cat >"$REPO/deploy/terraform/variables.tf" <<'VARIABLES'
variable "oke_kubernetes_version" {
  type    = string
  default = "v1.34.1"
}
VARIABLES
git -C "$REPO" init -q && git -C "$REPO" add -A

# The CLI: its pin is behind the newest release of the line the workflows float on.
printf 'v1.9.5\nv1.9.8\nv1.10.0\n' >"$CLI/list"
for tag in v1.9.5 v1.9.8; do
  printf 'module github.com/hashicorp/terraform\n\nrequire golang.org/x/text v0.20.0\n' >"$CLI/$tag.mod"
done
# The provider: the registry lists its releases, and each release tag carries its module file.
printf '{"versions": [{"version": "7.0.0"}, {"version": "6.1.2"}, {"version": "6.1.0"}, {"version": "6.2.0-beta"}]}\n' \
  >"$HTTP/registry.terraform.io/v1/providers/oracle/oci/versions"
printf 'module github.com/oracle/terraform-provider-oci\n\nrequire golang.org/x/net v0.30.0\n' >"$RAW/v6.1.0/go.mod"
printf 'module github.com/oracle/terraform-provider-oci\n\nrequire golang.org/x/net v0.33.0\n' >"$RAW/v6.1.2/go.mod"
printf 'module github.com/oracle/terraform-provider-oci\n\nrequire golang.org/x/net v0.33.0\n' >"$RAW/v7.0.0/go.mod"
# The plugin: its patch release still builds with the affected module, and its next minor does not.
printf 'v0.10.0\nv0.10.1\nv0.11.0\n' >"$PLUGIN/list"
for tag in v0.10.0 v0.10.1; do
  printf 'module github.com/terraform-linters/tflint-ruleset-terraform\n\nrequire github.com/hashicorp/hcl/v2 v2.20.0\n' \
    >"$PLUGIN/$tag.mod"
done
printf 'module github.com/terraform-linters/tflint-ruleset-terraform\n\nrequire github.com/hashicorp/hcl/v2 v2.21.0\n' \
  >"$PLUGIN/v0.11.0.mod"

# advisory ID NAME FIXED writes an OSV record for a Go module whose one range ends at FIXED.
advisory() {
  printf '{"id": "%s", "summary": "%s", "affected": [{"package": {"ecosystem": "Go", "name": "%s"},
    "ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "%s"}]}]}]}\n' \
    "$1" "a hole in $2" "$2" "$3" >"$VULNS/$1"
}
advisory GO-2026-2000 github.com/hashicorp/terraform 1.9.8
advisory GO-2026-3000 golang.org/x/net 0.33.0
advisory GO-2026-4000 github.com/hashicorp/hcl/v2 2.21.0
advisory GHSA-k111-1111-1111 k8s.io/kubernetes 1.34.2
cat >"$HTTP/osv-matches.json" <<'JSON'
{"Go|github.com/hashicorp/terraform|1.9.5": ["GO-2026-2000"], "Go|golang.org/x/net|0.30.0": ["GO-2026-3000"],
 "Go|github.com/hashicorp/hcl/v2|2.20.0": ["GO-2026-4000"], "Go|k8s.io/kubernetes|1.34.1": ["GHSA-k111-1111-1111"]}
JSON

# The OCI CLI answers with the Kubernetes versions the cluster service offers.
cat >"$WORK/bin/oci" <<'OCI'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_OCI_LOG"
cat "$FAKE_OKE_OFFERS"
OCI
chmod +x "$WORK/bin/oci"
echo '["v1.33.4", "v1.34.3", "v1.35.0"]' >"$WORK/offers.json"

run() {
  env PATH="$WORK/bin:$PATH" FAKE_HTTP_DIR="$HTTP" FAKE_HTTP_LOG="$WORK/http.log" FAKE_OCI_LOG="$WORK/oci.log" \
    FAKE_OKE_OFFERS="$WORK/offers.json" XDG_CACHE_HOME="$WORK/cache" \
    python3 "$REPO/scripts/dependency-fixes" terraform >"$WORK/out" 2>&1
}
out_has() { grep -qF -- "$1" "$WORK/out"; }

echo "dependency-fixes terraform:"

status=0
run || status=$?
patch_list="$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")"
newer_list="$(sed -n '/newer minor or major/,$p' "$WORK/out")"
if [ "$status" -eq 1 ] \
  && grep -qF 'terraform (terraform) github.com/hashicorp/terraform 1.9.5: GO-2026-2000 fixed in 1.9.8' <<<"$patch_list" \
  && grep -qF 'move deploy/terraform/.terraform-version to 1.9.8' <<<"$patch_list" \
  && grep -qF 'GET https://proxy.golang.org/github.com/hashicorp/terraform/@v/v1.9.8.mod' "$WORK/http.log"; then
  pass "the CLI's pin is refused when its line's patch fixes a hole, and the line the workflows float on is read"
else
  fail "the CLI's pin must be moved to its patch release (status=$status out=[$(cat "$WORK/out")])"
fi
if grep -qF 'built from (oracle/oci 6.1.0) golang.org/x/net v0.30.0: GO-2026-3000 fixed in oracle/oci 6.1.2' <<<"$patch_list" \
  && grep -qF 'move oracle/oci in deploy/terraform/.terraform.lock.hcl to 6.1.2' <<<"$patch_list" && ! out_has 'beta'; then
  pass "a provider's module file at its release tag is read, and its final patch release is the fix"
else
  fail "the provider's patch release must be named the fix (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'built from (github.com/terraform-linters/tflint-ruleset-terraform 0.10.0) github.com/hashicorp/hcl/v2 v2.20.0: GO-2026-4000 fixed in github.com/terraform-linters/tflint-ruleset-terraform v0.11.0' <<<"$newer_list"; then
  pass "a tflint plugin whose patch release still carries a hole is listed for Ivan at its next minor"
else
  fail "the plugin's newer-line fix must be listed (out=[$(cat "$WORK/out")])"
fi
if grep -qF 'kubernetes (deploy/terraform/variables.tf) k8s.io/kubernetes 1.34.1: GHSA-k111-1111-1111 fixed in 1.34.3' <<<"$patch_list" \
  && grep -qF 'move oke_kubernetes_version in deploy/terraform/variables.tf to v1.34.3' <<<"$patch_list"; then
  pass "a Kubernetes hole a patch fixes is answered by the earliest fixed version the cluster service offers"
else
  fail "the Kubernetes fix must be the version OKE offers (out=[$(cat "$WORK/out")] oci=[$(cat "$WORK/oci.log")])"
fi

echo '["v1.34.1", "v1.35.0"]' >"$WORK/offers.json"
status=0
run || status=$?
if ! grep -qF 'k8s.io/kubernetes' <<<"$(sed -n '/patch release of the line/,/newer minor or major/p' "$WORK/out")" \
  && out_has '1 advisories have no fixed release'; then
  pass "a Kubernetes patch the cluster service does not offer yet leaves nothing to take"
else
  fail "an unoffered Kubernetes patch must pass as no fix (status=$status out=[$(cat "$WORK/out")])"
fi

printf 'plugin "oci" {\n  enabled = true\n  version = "0.1.0"\n}\n' >>"$REPO/deploy/terraform/.tflint.hcl"
status=0
run || status=$?
if [ "$status" -eq 2 ] && out_has 'deploy/terraform/.tflint.hcl: plugin oci names a version or a source but not both'; then
  pass "a tflint plugin naming a version without its source fails the check as unreadable"
else
  fail "a plugin with no source must exit 2 (status=$status out=[$(cat "$WORK/out")])"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
