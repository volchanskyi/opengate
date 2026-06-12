#!/usr/bin/env bash
# scripts/arch-lint-flip.sh — per ADR-020 §5.4 warn→error auto-flip.
#
# Usage:
#   scripts/arch-lint-flip.sh [--check|--apply]
#
# --check (default): report which gates are eligible to flip and their state. Exit 0.
# --apply:           create marker files for gates that are clean and not yet flipped.
#
# Markers live at .claude/.markers/arch-lint-flipped/<gate>. When a marker
# exists, the gauntlet and the CI workflow run the gate in error mode (zero
# violations required) instead of the warn-mode baseline-snapshot pattern.
#
# Gates handled today:
#   depcruise           — eligible when web/dependency-cruiser.snapshot.json's
#                          `.warn` count is 0. Updating the snapshot is the
#                          dev-facing action that records "all violations fixed";
#                          --apply records the flip in a tracked marker.
#
# Deferred (per ADR-020 §5.4 mechanism notes):
#   eslint-boundaries   — requires eslint.config.js severity mutation; out of
#                          scope for the initial scaffolding ADR-020 PR.
#   cargo-deny          — bans/multiple-versions warns; covered by ADR-020
#                          amendment once the HTTP-dep inventory closes.
#
# Already-strict (no flip needed):
#   go-arch-lint        — runs in deny-by-default mode at the gauntlet today.
#   cargo-modules       — snapshot diff is binary; any mismatch fails.

set -euo pipefail

mode="${1:---check}"
case "$mode" in
  --check|--apply) ;;
  *) echo "usage: $0 [--check|--apply]" >&2; exit 2 ;;
esac

repo="$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
marker_dir="$repo/.claude/.markers/arch-lint-flipped"
mkdir -p "$marker_dir"

flipped_count=0

# ----------------------------------------------------------------------------
# Gate: depcruise
# ----------------------------------------------------------------------------
depcruise_snapshot="$repo/web/dependency-cruiser.snapshot.json"
depcruise_marker="$marker_dir/depcruise"

depcruise_state="unknown"
depcruise_warn="?"
if [ -f "$depcruise_marker" ]; then
  depcruise_state="flipped"
elif [ -f "$depcruise_snapshot" ]; then
  depcruise_warn=$(jq -r '.warn // 0' "$depcruise_snapshot")
  if [ "$depcruise_warn" = "0" ]; then
    depcruise_state="eligible"
  else
    depcruise_state="dirty"
  fi
else
  depcruise_state="no snapshot"
fi

case "$depcruise_state" in
  flipped)     printf 'gate: depcruise           — flipped (marker present)\n' ;;
  eligible)    printf 'gate: depcruise           — eligible to flip (warn=0)\n' ;;
  dirty)       printf 'gate: depcruise           — dirty (warn=%s)\n' "$depcruise_warn" ;;
  "no snapshot") printf 'gate: depcruise           — no snapshot at %s\n' "$depcruise_snapshot" ;;
  *)           printf 'gate: depcruise           — %s\n' "$depcruise_state" ;;
esac

if [ "$mode" = "--apply" ] && [ "$depcruise_state" = "eligible" ]; then
  cat >"$depcruise_marker" <<EOF
# ADR-020 §5.4 flip marker for the depcruise gate.
#
# Created by scripts/arch-lint-flip.sh --apply on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Trigger: web/dependency-cruiser.snapshot.json's .warn count reached zero.
#
# While this file is present, the gauntlet and CI run depcruise in strict
# error mode (zero violations required) instead of the warn-mode baseline-
# snapshot pattern. Remove the file to revert to warn mode.
EOF
  printf '  → flipped: created marker %s\n' "${depcruise_marker#"$repo/"}"
  flipped_count=$((flipped_count + 1))
fi

# ----------------------------------------------------------------------------
# Gate: eslint-boundaries (ADR-020 §5.4)
#
# State machine driven by web/eslint.config.js's `boundaries/dependencies`
# severity token AND the marker file:
#
#   - severity 'warn'  AND no marker  → eligible
#   - severity 'error' OR  marker     → flipped
#   - missing config                  → no config
#
# `--apply` on eligible mutates the severity warn → error AND writes the
# marker atomically. Re-apply is idempotent: severity already 'error'
# reports as flipped without further mutation.
# ----------------------------------------------------------------------------
eslint_config="$repo/web/eslint.config.js"
eslint_marker="$marker_dir/eslint-boundaries"

# Fixed-string patterns; anchored on the rule key so we don't match a
# stray severity literal elsewhere in the file.
eslint_warn_pat="'boundaries/dependencies': ['warn'"
eslint_error_pat="'boundaries/dependencies': ['error'"

eslint_state="unknown"
if [ -f "$eslint_marker" ]; then
  eslint_state="flipped"
elif [ ! -f "$eslint_config" ]; then
  eslint_state="no config"
elif grep -qF "$eslint_error_pat" "$eslint_config"; then
  eslint_state="flipped"
elif grep -qF "$eslint_warn_pat" "$eslint_config"; then
  eslint_state="eligible"
fi

case "$eslint_state" in
  flipped)     printf 'gate: eslint-boundaries   — flipped (marker or severity=error)\n' ;;
  eligible)    printf 'gate: eslint-boundaries   — eligible to flip (severity=warn, zero violations)\n' ;;
  "no config") printf 'gate: eslint-boundaries   — no config at %s\n' "${eslint_config#"$repo/"}" ;;
  *)           printf 'gate: eslint-boundaries   — %s\n' "$eslint_state" ;;
esac

if [ "$mode" = "--apply" ] && [ "$eslint_state" = "eligible" ]; then
  # Atomic: sed into a temp, validate the token actually flipped, then mv.
  tmp="$(mktemp)"
  # `#` delimiter so the `/` inside `boundaries/dependencies` needs no
  # escaping. `\[` escapes the literal `[` in the BRE pattern.
  sed "s#'boundaries/dependencies': \\['warn'#'boundaries/dependencies': ['error'#" \
      "$eslint_config" > "$tmp"
  if grep -qF "$eslint_error_pat" "$tmp" && ! grep -qF "$eslint_warn_pat" "$tmp"; then
    mv "$tmp" "$eslint_config"
    cat >"$eslint_marker" <<EOF
# ADR-020 §5.4 flip marker for the eslint-boundaries gate.
#
# Created by scripts/arch-lint-flip.sh --apply on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Trigger: web/eslint.config.js's boundaries/dependencies severity reached
# zero violations and was promoted from 'warn' to 'error'.
#
# While this file is present, the gauntlet and CI treat the eslint-boundaries
# gate as strict. Remove the file (and revert the severity to 'warn') to
# revert the flip.
EOF
    printf '  → flipped: created marker %s and mutated severity warn → error\n' "${eslint_marker#"$repo/"}"
    flipped_count=$((flipped_count + 1))
  else
    rm -f "$tmp"
    printf '  → flip aborted: sed did not produce expected severity token in %s\n' "${eslint_config#"$repo/"}" >&2
  fi
fi

# ----------------------------------------------------------------------------
# Gate: cargo-deny (ADR-020 §5.4, Amendment 1)
#
# State machine driven by agent/deny.toml's `multiple-versions` AND
# `wildcards` severity tokens AND the marker file:
#
#   - both severities 'warn'  AND no marker  → eligible
#   - both severities 'deny'  OR  marker     → flipped
#   - missing config                         → no config
#
# `--apply` on eligible mutates both severity tokens warn → deny atomically
# AND writes the marker. The cargo-deny tool itself reads deny.toml at
# `cd agent && cargo deny check` time (gauntlet step 9 + 18); flipping the
# severities makes any new violation fail the gate instead of warning.
# ----------------------------------------------------------------------------
deny_config="$repo/agent/deny.toml"
deny_marker="$marker_dir/cargo-deny"

deny_mv_warn='multiple-versions = "warn"'
deny_mv_deny='multiple-versions = "deny"'
deny_wc_warn='wildcards = "warn"'
deny_wc_deny='wildcards = "deny"'

deny_state="unknown"
if [ -f "$deny_marker" ]; then
  deny_state="flipped"
elif [ ! -f "$deny_config" ]; then
  deny_state="no config"
elif grep -qF "$deny_mv_deny" "$deny_config" && grep -qF "$deny_wc_deny" "$deny_config"; then
  deny_state="flipped"
elif grep -qF "$deny_mv_warn" "$deny_config" || grep -qF "$deny_wc_warn" "$deny_config"; then
  deny_state="eligible"
fi

case "$deny_state" in
  flipped)     printf 'gate: cargo-deny          — flipped (marker or both severities=deny)\n' ;;
  eligible)    printf 'gate: cargo-deny          — eligible to flip (severity=warn)\n' ;;
  "no config") printf 'gate: cargo-deny          — no config at %s\n' "${deny_config#"$repo/"}" ;;
  *)           printf 'gate: cargo-deny          — %s\n' "$deny_state" ;;
esac

if [ "$mode" = "--apply" ] && [ "$deny_state" = "eligible" ]; then
  tmp="$(mktemp)"
  sed -e 's/^multiple-versions = "warn"/multiple-versions = "deny"/' \
      -e 's/^wildcards = "warn"/wildcards = "deny"/' \
      "$deny_config" > "$tmp"
  if grep -qF "$deny_mv_deny" "$tmp" && grep -qF "$deny_wc_deny" "$tmp"; then
    mv "$tmp" "$deny_config"
    cat >"$deny_marker" <<EOF
# ADR-020 §5.4 flip marker for the cargo-deny gate.
#
# Created by scripts/arch-lint-flip.sh --apply on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Trigger: agent/deny.toml's multiple-versions + wildcards severities reached
# zero violations and were promoted from 'warn' to 'deny'.
#
# While this file is present, the gauntlet and CI treat the cargo-deny gate
# as strict. Remove the file (and revert the severities to 'warn') to
# revert the flip.
EOF
    printf '  → flipped: created marker %s and mutated severities warn → deny\n' "${deny_marker#"$repo/"}"
    flipped_count=$((flipped_count + 1))
  else
    rm -f "$tmp"
    printf '  → flip aborted: sed did not produce expected severity tokens in %s\n' "${deny_config#"$repo/"}" >&2
  fi
elif [ "$mode" = "--apply" ] && [ "$deny_state" = "flipped" ] && [ ! -f "$deny_marker" ]; then
  # Reconcile: deny.toml severities were edited to 'deny' out-of-band (e.g.
  # in the same commit that introduces this gate). Record the marker so the
  # audit trail matches the config state.
  cat >"$deny_marker" <<EOF
# ADR-020 §5.4 flip marker for the cargo-deny gate.
#
# Created by scripts/arch-lint-flip.sh --apply on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Trigger: agent/deny.toml severities were already at 'deny' when --apply ran;
# the marker reconciles the audit trail with the config state.
EOF
  printf '  → reconciled: created marker %s (severities already at deny)\n' "${deny_marker#"$repo/"}"
  flipped_count=$((flipped_count + 1))
fi

# ----------------------------------------------------------------------------
# Already-strict gates.
# ----------------------------------------------------------------------------
printf 'gate: go-arch-lint        — already strict at the gauntlet (no flip needed)\n'
printf 'gate: cargo-modules       — already strict at the gauntlet (no flip needed)\n'

if [ "$mode" = "--apply" ]; then
  if [ "$flipped_count" -gt 0 ]; then
    printf '\n%d gate(s) flipped. Commit the marker file(s) under %s\n' "$flipped_count" "${marker_dir#"$repo/"}"
  else
    printf '\nNo gates flipped.\n'
  fi
fi

exit 0
