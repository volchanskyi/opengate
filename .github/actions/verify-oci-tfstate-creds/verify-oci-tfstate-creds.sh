#!/usr/bin/env bash
# Validate OCI backend and API credential shape without exposing secret values.

set -euo pipefail

check_dns_and_s3() {
  local namespace="${OCI_TFSTATE_NAMESPACE:-}"
  local region="${OCI_REGION:-}"
  local access_key="${TFSTATE_S3_ACCESS_KEY:-}"
  local secret_key="${TFSTATE_S3_SECRET_KEY:-}"
  local namespace_host
  local region_host
  local namespace_ok=0
  local region_ok=0
  local stale=0

  if [ -z "$namespace" ]; then
    echo "::error::OCI_TFSTATE_NAMESPACE secret is empty — backend URL will not resolve"
    exit 1
  fi
  if [ -z "$region" ]; then
    echo "::error::OCI_REGION secret is empty — backend URL will not resolve"
    exit 1
  fi

  echo "OCI_TFSTATE_NAMESPACE length: ${#namespace}"
  echo "OCI_REGION length: ${#region}"

  namespace_host="${namespace}.compat.objectstorage.${region}.oraclecloud.com"
  region_host="objectstorage.${region}.oraclecloud.com"
  echo "::add-mask::${namespace_host}"
  echo "::add-mask::${region_host}"

  if getent hosts "$namespace_host" >/dev/null 2>&1; then
    namespace_ok=1
  fi
  if getent hosts "$region_host" >/dev/null 2>&1; then
    region_ok=1
  fi
  echo "namespace-scoped endpoint resolves: ${namespace_ok}"
  echo "region-only endpoint resolves: ${region_ok}"

  if [ "$namespace_ok" = "0" ] && [ "$region_ok" = "0" ]; then
    echo "::warning::Both OCI endpoints fail DNS — likely wrong OCI_REGION secret or OCI region outage. terraform init will retry."
  elif [ "$namespace_ok" = "0" ] && [ "$region_ok" = "1" ]; then
    echo "::warning::Region resolves but namespace-scoped endpoint does not — likely wrong OCI_TFSTATE_NAMESPACE secret. Verify the tenancy's object-storage namespace matches the secret."
  fi

  echo "TFSTATE_S3_ACCESS_KEY length: ${#access_key}"
  echo "TFSTATE_S3_SECRET_KEY length: ${#secret_key}"
  if [ -z "$access_key" ]; then
    echo "::warning::TFSTATE_S3_ACCESS_KEY is empty — terraform init will 403 with SignatureDoesNotMatch."
    stale=1
  elif [ "${#access_key}" -lt 20 ]; then
    echo "::warning::TFSTATE_S3_ACCESS_KEY length (${#access_key}) looks short — OCI Customer Secret Key access keys are ~32 chars. Secret may be truncated."
    stale=1
  fi
  if [ -z "$secret_key" ]; then
    echo "::warning::TFSTATE_S3_SECRET_KEY is empty — terraform init will 403 with SignatureDoesNotMatch."
    stale=1
  elif [ "${#secret_key}" -lt 24 ]; then
    echo "::warning::TFSTATE_S3_SECRET_KEY length (${#secret_key}) looks short — OCI secret keys are typically ~28 chars. Secret may be truncated."
    stale=1
  fi

  if [ "$stale" = "1" ]; then
    echo "::warning::S3 credentials look stale or truncated. Rotate the Customer Secret Key in OCI Console for the workflow's IAM user, then re-paste BOTH halves into TFSTATE_S3_ACCESS_KEY and TFSTATE_S3_SECRET_KEY in repo secrets (no whitespace, no trailing newline)."
  else
    echo "S3 credentials shape OK (lengths plausible). A downstream 403 SignatureDoesNotMatch still implies a key-pair mismatch — rotate both halves together."
  fi
}

check_api_key() {
  local tenancy_ocid="${OCI_TENANCY_OCID:-}"
  local user_ocid="${OCI_DRIFT_USER_OCID:-}"
  local fingerprint="${OCI_DRIFT_FINGERPRINT:-}"
  local private_key="${OCI_DRIFT_PRIVATE_KEY:-}"
  local computed_fingerprint=""

  if [ -z "$tenancy_ocid" ]; then
    echo "::warning::OCI_TENANCY_OCID is empty"
  elif ! [[ "$tenancy_ocid" =~ ^ocid1\.tenancy\.oc1\. ]]; then
    echo "::warning::OCI_TENANCY_OCID does not start with 'ocid1.tenancy.oc1.' — wrong secret type?"
  else
    echo "OCI_TENANCY_OCID shape OK"
  fi
  if [ -z "$user_ocid" ]; then
    echo "::warning::OCI_DRIFT_USER_OCID is empty"
  elif ! [[ "$user_ocid" =~ ^ocid1\.user\.oc1\. ]]; then
    echo "::warning::OCI_DRIFT_USER_OCID does not start with 'ocid1.user.oc1.' — wrong secret type?"
  else
    echo "OCI_DRIFT_USER_OCID shape OK"
  fi

  if [ -z "$fingerprint" ]; then
    echo "::warning::OCI_DRIFT_FINGERPRINT is empty"
  elif ! [[ "$fingerprint" =~ ^[0-9a-f]{2}(:[0-9a-f]{2}){15}$ ]]; then
    echo "::warning::OCI_DRIFT_FINGERPRINT does not match expected shape (32 lowercase hex chars in 16 colon-separated octets). Got length ${#fingerprint}."
  else
    echo "OCI_DRIFT_FINGERPRINT shape OK"
  fi

  if [ -z "$private_key" ]; then
    echo "::error::OCI_DRIFT_PRIVATE_KEY is empty — terraform refresh will 401."
    exit 1
  fi
  if printf '%s' "$private_key" | grep -qF '\n'; then
    printf '%s\n' "::warning::OCI_DRIFT_PRIVATE_KEY appears to contain literal \\n characters — paste may have escaped newlines. Re-paste raw PEM contents."
  fi
  if ! printf '%s' "$private_key" | grep -q -- "-----BEGIN .*PRIVATE KEY-----"; then
    echo "::error::OCI_DRIFT_PRIVATE_KEY missing BEGIN PRIVATE KEY marker — paste truncated or header stripped."
    exit 1
  fi
  if ! printf '%s' "$private_key" | grep -q -- "-----END .*PRIVATE KEY-----"; then
    echo "::error::OCI_DRIFT_PRIVATE_KEY missing END PRIVATE KEY marker — paste truncated."
    exit 1
  fi
  if ! printf '%s' "$private_key" | openssl pkey -noout 2>/dev/null; then
    echo "::error::OCI_DRIFT_PRIVATE_KEY fails openssl parse — PEM corrupted (line wraps, escaped newlines, or wrong key format)."
    exit 1
  fi
  echo "OCI_DRIFT_PRIVATE_KEY parses cleanly ✓"

  computed_fingerprint="$(
    printf '%s' "$private_key" \
      | openssl pkey -pubout -outform DER 2>/dev/null \
      | openssl md5 -c 2>/dev/null \
      | awk '{print $NF}'
  )" || computed_fingerprint=""

  if [ -z "$computed_fingerprint" ]; then
    echo "::warning::Could not compute fingerprint from OCI_DRIFT_PRIVATE_KEY (openssl pipeline failed). Skipping cross-check."
  elif [ -z "$fingerprint" ]; then
    echo "::warning::Skipping fingerprint cross-check: OCI_DRIFT_FINGERPRINT is empty."
  elif [ "$computed_fingerprint" != "$fingerprint" ]; then
    echo "::error::OCI_DRIFT_FINGERPRINT does NOT match the fingerprint computed from OCI_DRIFT_PRIVATE_KEY. One half of the API key pair is stale."
    echo "::error::  computed from key: ${computed_fingerprint}"
    echo "::error::  stored secret:     ${fingerprint}"
    echo "::error::Fix: in OCI Console, view the API key for the workflow's user. If the fingerprint shown there matches 'computed from key', update OCI_DRIFT_FINGERPRINT to that value. Otherwise delete the key and generate a new one, then update BOTH OCI_DRIFT_FINGERPRINT and OCI_DRIFT_PRIVATE_KEY in repo secrets together."
    exit 1
  else
    echo "OCI API key fingerprint matches private key ✓"
  fi
}

case "${1:-}" in
  dns-and-s3) check_dns_and_s3 ;;
  api-key) check_api_key ;;
  *)
    echo "usage: $0 {dns-and-s3|api-key}" >&2
    exit 2
    ;;
esac
