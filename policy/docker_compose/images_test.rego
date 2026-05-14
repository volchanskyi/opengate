package main

# --- POSITIVE: pinned tags pass --------------------------------------------

test_pinned_tag_passes {
	count(deny) == 0 with input as {"services": {
		"postgres": {"image": "postgres:17-alpine"},
		"caddy":    {"image": "caddy:2-alpine"},
	}}
}

test_image_with_env_var_tag_passes {
	# The literal contains `:` (the env-var-template colon counts), so the
	# no-tag rule does not fire. Catching `:${VAR:-latest}` fallbacks is the
	# job of a render-time lint, not this static policy.
	count(deny) == 0 with input as {"services": {
		"server": {"image": "ghcr.io/volchanskyi/opengate-server:${IMAGE_TAG:-latest}"},
	}}
}

test_build_service_without_image_passes {
	# Services with a build: directive can omit `image:`.
	count(deny) == 0 with input as {"services": {
		"web": {"build": {"context": "."}},
	}}
}

# --- NEGATIVE: literal :latest and no-tag are denied -----------------------

test_literal_latest_denied {
	deny[msg] with input as {"services": {
		"x": {"image": "postgres:latest"},
	}}
	contains(msg, "uses literal :latest")
}

test_no_tag_denied {
	deny[msg] with input as {"services": {
		"x": {"image": "redis"},
	}}
	contains(msg, "has no tag")
}
