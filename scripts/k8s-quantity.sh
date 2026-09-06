#!/usr/bin/env bash
# Turn a Kubernetes quantity into the plain number a reader can compare.
#
# The cluster states a container's limits in its own notation: a processor share
# as "250m", memory as "384Mi". Both are exact and neither is a number — put
# straight into an evidence bundle they land in a numeric field as a string, or
# as whatever the leading digits happened to be, and the figure a later reader
# compares two runs by is then 250 processors and 384 bytes.
#
# So the conversion happens once, here, where it is testable, rather than in a
# workflow line nobody can run.
#
#   k8s-quantity.sh cpu 250m      → 0.25       (processors, fractional)
#   k8s-quantity.sh memory 384Mi  → 402653184  (bytes)
#
# A quantity it cannot read fails rather than printing zero. Zero processors and
# zero bytes is the smallest machine ever measured, and a bundle carrying it
# would be refused for the wrong reason — or worse, accepted.
set -euo pipefail

usage() {
  echo "usage: $0 <cpu|memory> <quantity>" >&2
}

# cpu_cores turns a processor quantity into a fraction of one processor. The
# only suffix Kubernetes uses here is "m" for milli.
cpu_cores() {
  local raw="$1"
  case "$raw" in
    *m)
      awk -v v="${raw%m}" 'BEGIN { printf "%g", v / 1000 }'
      ;;
    *)
      awk -v v="$raw" 'BEGIN { printf "%g", v }'
      ;;
  esac
}

# memory_bytes turns a memory quantity into bytes. Kubernetes writes binary
# multiples with an "i" (Ki, Mi, Gi) and decimal ones without (k, M, G), and
# they are different numbers — 384Mi is 402,653,184 bytes and 384M is
# 384,000,000, a difference of eighteen megabytes that a memory ceiling is
# stated to the byte precisely to avoid.
memory_bytes() {
  local raw="$1"
  local mult=1 digits="$raw"
  case "$raw" in
    *Ki) mult=1024 digits="${raw%Ki}" ;;
    *Mi) mult=$((1024 ** 2)) digits="${raw%Mi}" ;;
    *Gi) mult=$((1024 ** 3)) digits="${raw%Gi}" ;;
    *Ti) mult=$((1024 ** 4)) digits="${raw%Ti}" ;;
    *k) mult=1000 digits="${raw%k}" ;;
    *M) mult=$((1000 ** 2)) digits="${raw%M}" ;;
    *G) mult=$((1000 ** 3)) digits="${raw%G}" ;;
    *T) mult=$((1000 ** 4)) digits="${raw%T}" ;;
  esac
  awk -v v="$digits" -v m="$mult" 'BEGIN { printf "%d", v * m }'
}

# numeric reports whether what is left after the suffix is a number at all.
numeric() {
  [[ "$1" =~ ^[0-9]+([.][0-9]+)?$ ]]
}

main() {
  if [ "$#" -ne 2 ]; then
    usage
    return 2
  fi

  local kind="$1" raw="$2" stripped
  if [ -z "$raw" ]; then
    echo "::error::there is no quantity to convert, so the figure would reach the evidence as a zero." >&2
    return 1
  fi

  stripped="${raw%%[a-zA-Z]*}"
  if ! numeric "$stripped"; then
    echo "::error::'$raw' is not a quantity this reads, and a figure it guessed at is worse than none." >&2
    return 1
  fi

  case "$kind" in
    cpu) cpu_cores "$raw" ;;
    memory) memory_bytes "$raw" ;;
    *)
      usage
      return 2
      ;;
  esac
  echo
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
