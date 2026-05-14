package main

# --- POSITIVE: allowed-unpinned owners + SHA-pinned third-party + local refs ---

test_actions_owned_passes {
	# `actions/checkout@v6` is on the allowlist (`actions/*`).
	count(deny) == 0 with input as {"jobs": {"build": {"steps": [
		{"uses": "actions/checkout@v6"},
	]}}}
}

test_github_owned_passes {
	count(deny) == 0 with input as {"jobs": {"build": {"steps": [
		{"uses": "github/codeql-action/init@v4"},
	]}}}
}

test_docker_owned_passes {
	count(deny) == 0 with input as {"jobs": {"build": {"steps": [
		{"uses": "docker/build-push-action@v7"},
	]}}}
}

test_local_action_passes {
	count(deny) == 0 with input as {"jobs": {"build": {"steps": [
		{"uses": "./.github/actions/oci-ssh-setup"},
	]}}}
}

test_sha_pinned_third_party_passes {
	count(deny) == 0 with input as {"jobs": {"build": {"steps": [
		{"uses": "Swatinem/rust-cache@e18b497796c12c097a38f9edb9d0641fb99eee32"},
	]}}}
}

# --- NEGATIVE: tag-pinned third party fires deny ---

test_tag_pinned_third_party_denied {
	deny[msg] with input as {"jobs": {"build": {"steps": [
		{"uses": "Swatinem/rust-cache@v2"},
	]}}}
	contains(msg, "must be pinned to a 40-char SHA")
}

test_branch_ref_third_party_denied {
	deny[msg] with input as {"jobs": {"build": {"steps": [
		{"uses": "dtolnay/rust-toolchain@stable"},
	]}}}
	contains(msg, "must be pinned to a 40-char SHA")
}

test_short_sha_third_party_denied {
	# 7-char short SHAs are NOT acceptable; only full 40-char.
	deny[msg] with input as {"jobs": {"build": {"steps": [
		{"uses": "Swatinem/rust-cache@e18b497"},
	]}}}
	contains(msg, "must be pinned to a 40-char SHA")
}
