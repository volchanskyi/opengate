#!/usr/bin/env bash
# Reads the kernel's seconds since boot, a clock the wall clock's steps never move backwards.
#
# Usage:  . scripts/lib/step-clock.sh   then   start="$(step_clock)" … $(($(step_clock) - start))

# step_clock prints the whole seconds the kernel has counted since boot.
step_clock() {
  local up _
  read -r up _ </proc/uptime
  printf '%s\n' "${up%.*}"
}
