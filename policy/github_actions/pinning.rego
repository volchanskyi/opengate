# GitHub Actions SHA-pinning policy.
#
# Third-party actions must be referenced by 40-char Git SHA, not by tag or
# branch name. Tag references are mutable (an attacker who controls the
# upstream repo can re-point a tag to malicious code) and have been the
# vector of multiple supply-chain incidents (tj-actions/changed-files Mar 2025,
# reviewdog 2025, etc.). SHA pins close that vector by trading mutable refs
# for immutable commit-hash refs.
#
# Allowlist (`owners` set below): action publishers whose tags are governed by
# stricter mutability controls — GitHub-owned (actions, github), Oracle, and
# Docker official. Local actions referenced as `./...` are also exempt
# (they live in this repo and cannot be retro-pointed by a third party).
#
# Input shape: parsed workflow YAML, e.g.
#   {"jobs": {"<JOB>": {"steps": [{"uses": "owner/repo@ref"}, ...]}}}.

package main

# Owners whose actions are trusted enough to reference by tag/branch.
allowed_unpinned_owners := {"actions", "github", "oracle-actions", "docker"}

deny[msg] {
	job := input.jobs[job_name]
	step := job.steps[i]
	uses := step.uses
	is_remote_action(uses)
	owner := action_owner(uses)
	not allowed_unpinned_owners[owner]
	ref := action_ref(uses)
	not is_sha40(ref)
	msg := sprintf("jobs.%v.steps[%v]: third-party action %q must be pinned to a 40-char SHA, not %q", [job_name, i, uses, ref])
}

# --- helpers ---------------------------------------------------------------

# True if the `uses:` string references a remote action (not `./local-path`).
is_remote_action(uses) {
	not startswith(uses, "./")
	not startswith(uses, "docker://")  # docker:// references are pinned by image digest, not by GH SHA
	contains(uses, "@")
}

# `Swatinem/rust-cache@v2` → `Swatinem`
action_owner(uses) = owner {
	parts := split(uses, "/")
	owner := parts[0]
}

# `Swatinem/rust-cache@v2` → `v2`
# `github/codeql-action/analyze@v4` → `v4`
action_ref(uses) = ref {
	parts := split(uses, "@")
	ref := parts[count(parts) - 1]
}

# True if the string is exactly 40 hex characters.
is_sha40(ref) {
	count(ref) == 40
	regex.match(`^[0-9a-f]{40}$`, ref)
}
