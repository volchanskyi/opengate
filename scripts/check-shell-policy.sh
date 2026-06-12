#!/usr/bin/env bash
# Enforce Bash shebang, strict-mode, library, and option-disable policy.

set -euo pipefail

ROOT="${SHELL_POLICY_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
MANIFEST="${SHELL_POLICY_MANIFEST:-$ROOT/.claude/shell-policy.exceptions}"
ERRORS=0

declare -A CLASS_BY_PATH=()
declare -A ALLOWED_DISABLES_BY_PATH=()

report() {
  printf 'shell-policy: %s\n' "$1" >&2
  ERRORS=$((ERRORS + 1))
}

if [[ ! -f "$MANIFEST" ]]; then
  report "manifest does not exist: $MANIFEST"
else
  while IFS=$'\t' read -r class path allowed_disables reason extra; do
    [[ -n "$class" ]] || continue
    [[ "$class" == \#* ]] && continue

    if [[ -z "$path" || -z "$allowed_disables" || -z "$reason" || -n "$extra" ]]; then
      report "invalid manifest row: expected class<TAB>path<TAB>allowed-options<TAB>reason"
      continue
    fi
    if [[ "$class" != "standalone" && "$class" != "aggregate" && "$class" != "library" ]]; then
      report "invalid class '$class' for $path"
      continue
    fi
    if [[ -n "${CLASS_BY_PATH[$path]:-}" ]]; then
      report "duplicate manifest entry: $path"
      continue
    fi
    if ! git -C "$ROOT" ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
      report "stale manifest entry: $path"
      continue
    fi
    if [[ "$path" != *.sh ]]; then
      report "manifest entry is not a shell script: $path"
      continue
    fi

    CLASS_BY_PATH["$path"]="$class"
    ALLOWED_DISABLES_BY_PATH["$path"]="$allowed_disables"
  done <"$MANIFEST"
fi

while IFS= read -r -d '' path; do
  file="$ROOT/$path"
  class="${CLASS_BY_PATH[$path]:-standalone}"
  allowed_disables="${ALLOWED_DISABLES_BY_PATH[$path]:--}"
  first_line="$(head -n 1 "$file")"
  option_line="$(awk '/^[[:space:]]*set -[A-Za-z]+([[:space:]]+pipefail)?[[:space:]]*$/ { print; exit }' "$file")"

  if [[ "$first_line" != "#!/usr/bin/env bash" ]]; then
    report "$path must use the Bash shebang '#!/usr/bin/env bash'"
  fi

  case "$class" in
    standalone)
      if [[ "$option_line" != "set -euo pipefail" && "$option_line" != "set -Eeuo pipefail" ]]; then
        report "$path must declare standalone strict mode (set -euo pipefail)"
      fi
      ;;
    aggregate)
      if [[ "$option_line" != "set -uo pipefail" ]]; then
        report "$path is classified aggregate and must declare 'set -uo pipefail'"
      fi
      ;;
    library)
      if grep -nE '^(set|shopt|trap)([[:space:]]|$)' "$file" >/dev/null; then
        report "$path mutates caller options or traps at file scope"
      fi
      ;;
  esac

  while IFS= read -r option_disable; do
    [[ -n "$option_disable" ]] || continue
    option="${option_disable#set +}"
    if [[ "$allowed_disables" == "-" || ",$allowed_disables," != *",$option,"* ]]; then
      report "$path contains unapproved option disable '$option_disable'"
    fi
  done < <(grep -oE 'set[[:space:]]+\+[A-Za-z]+' "$file" || true)
done < <(git -C "$ROOT" ls-files -z -- '*.sh')

if [[ "$ERRORS" -gt 0 ]]; then
  printf 'shell-policy: %d violation(s)\n' "$ERRORS" >&2
  exit 1
fi

printf 'shell-policy: clean\n'
