#!/usr/bin/env bash
# Reject reintroduction of the retired CI path to the decommissioned compose VM.

set -euo pipefail

repo_root="$(cd "${1:-$(dirname "${BASH_SOURCE[0]}")/..}" && pwd)"
scan_roots=(
  "$repo_root/.github/workflows"
  "$repo_root/.github/actions"
  "$repo_root/scripts"
)
excluded_files=(
  "$repo_root/scripts/no-vm-ssh-guard.sh"
  "$repo_root/scripts/tests/no-vm-ssh-guard.test.sh"
)
files=()

for scan_root in "${scan_roots[@]}"; do
  if [[ -d "$scan_root" ]]; then
    while IFS= read -r -d '' file; do
      skip=false
      for excluded_file in "${excluded_files[@]}"; do
        if [[ "$file" == "$excluded_file" ]]; then
          skip=true
          break
        fi
      done
      if [[ "$skip" == false ]]; then
        files+=("$file")
      fi
    done < <(find "$scan_root" -type f -print0)
  fi
done

if [[ "${#files[@]}" -eq 0 ]]; then
  exit 0
fi

labels=(
  "retired SSH composite action"
  "retired SSH host alias"
  "retired Loki transport"
  "retired VM deploy root"
  "retired cutover gate"
  "retired SSH-only secret"
)
patterns=(
  'oci-ssh-(setup|teardown)'
  'deploy-target'
  'LOKI_PUSH_MODE|ssh-docker|opengate-monitoring_monitoring'
  'DEPLOY_DIR|/opt/opengate'
  'K8S_CUTOVER'
  'DEPLOY_SSH_PRIVATE_KEY|DEPLOY_HOST|OCI_CD_NSG_ID'
)
remediations=(
  "Use .github/actions/oci-kube-setup and kubectl."
  "Run the operation through the Kubernetes API."
  "Use the in-cluster Loki Service through kubectl."
  "Keep CI deployment state in Helm and Kubernetes resources."
  "The Kubernetes deployment path is unconditional."
  "Use OCI API and OKE cluster credentials only."
)

found=0
for index in "${!patterns[@]}"; do
  matches="$(grep -IHnE -- "${patterns[$index]}" "${files[@]}" || true)"
  if [[ -z "$matches" ]]; then
    continue
  fi

  found=1
  printf 'Retired VM SSH path detected (%s):\n' "${labels[$index]}" >&2
  while IFS= read -r match; do
    printf '  %s\n' "${match#"$repo_root"/}" >&2
  done <<<"$matches"
  printf 'Remediation: %s\n\n' "${remediations[$index]}" >&2
done

exit "$found"
