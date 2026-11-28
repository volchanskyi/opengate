#!/usr/bin/env bash
# The processors a speed job never measures on, one a line, each matched as part of the model
# lscpu names. Sourced by scripts/runner-processor.sh and its test.

refused_processors() {
  echo "AMD EPYC 7763"
}
