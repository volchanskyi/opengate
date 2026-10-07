# Third-party actions are pinned to a 40-char SHA because tag references are mutable.
# The owners in allowed_unpinned_owners and local ./ actions are exempt.

package main

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

is_remote_action(uses) {
	not startswith(uses, "./")
	not startswith(uses, "docker://") # docker:// references are pinned by image digest
	contains(uses, "@")
}

action_owner(uses) = owner {
	parts := split(uses, "/")
	owner := parts[0]
}

action_ref(uses) = ref {
	parts := split(uses, "@")
	ref := parts[count(parts) - 1]
}

is_sha40(ref) {
	count(ref) == 40
	regex.match(`^[0-9a-f]{40}$`, ref)
}
