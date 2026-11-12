#!/usr/bin/env bash
# The server's and the database's levels as canonical rows: each a share of its own allowance,
# averaged over the phases the run measured (every phase, where it marks none).

# levels_rows prints the two rows as a JSON array from a bundle and the database's levels, read
# from the file given or else from the bundle's own; a reading the run could not take is null.
levels_rows() { # bundle [database.json]
  local bundle="$1" database="${2:-}" db='null'
  if [ -n "$database" ] && [ -s "$database" ]; then
    db="$(jq -c '.' "$database")"
  fi
  jq -c --argjson file "$db" '
    ($file // .database_levels // {}) as $db
    | ([.phases[]? | select(.measured)] | if length == 0 then null else . end) as $marked
    | ($marked // [.phases[]?]) as $measured
    | def mean(f): [$measured[] | f | select(. != null)] | if length == 0 then null else add / length end;
    (mean(.target_resident_bytes)) as $resident
    | {commit: (.run.commit // "unknown"), env: (.run.environment // "unknown"),
       timestamp: (.run.finished_at // null), target_cpus: (.target.cpus // null)} as $base
    | [ $base + {source: "service", scenario: "server", phase: "measured",
                 cpu_percent: mean(.target_busy_percent),
                 memory_percent: (if $resident == null or (.target.memory_bytes // 0) == 0 then null
                                  else $resident / .target.memory_bytes * 100 end),
                 cpu_cap: (.target.cpus // null), memory_bytes: (.target.memory_bytes // null)},
        $base + {source: "service", scenario: "database", phase: "measured",
                 cpu_percent: ($db.cpu_percent // null), memory_percent: ($db.memory_percent // null),
                 cpu_cap: ($db.cpu_cap // null), memory_cap: ($db.memory_cap // null)} ]
  ' "$bundle"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  echo "loadtest-levels.sh is a sourced library" >&2
  exit 2
fi
