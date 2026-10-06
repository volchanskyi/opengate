#!/usr/bin/env bash
# Converts a Kubernetes quantity into a plain number, failing on a quantity it cannot read.
#
# Usage:
#   k8s-quantity.sh cpu 250m      prints 0.25, in processors
#   k8s-quantity.sh memory 384Mi  prints 402653184, in bytes
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

# memory_bytes turns a memory quantity into bytes; the binary suffixes (Mi) and the decimal ones
# (M) are different multiples.
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
