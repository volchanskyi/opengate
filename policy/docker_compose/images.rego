# Docker Compose image pinning policy (MVP).
#
# Today's rules:
#   1. `image:` must not end in literal `:latest`.
#   2. `image:` must have a tag (i.e. contain a `:`).
#
# Deliberately NOT enforced yet (deferred to a follow-up):
#   - `@sha256:` digest pin requirement for non-build services. Today's
#     compose files pin tags but not digests (postgres:17-alpine, caddy:2-alpine,
#     etc.). Tightening the policy without first migrating to digest-pinned
#     images would break every deploy. Tracked in techdebt.md.
#   - `${VAR:-latest}` fallback defaults (e.g. `IMAGE_TAG:-latest` in
#     ghcr.io/volchanskyi/opengate-server). The policy CANNOT see what value
#     the variable resolves to, only the literal text. Catching this is the
#     job of a separate compose-render lint that runs `docker compose config`
#     and checks the output — out of scope for the static-policy layer.
#
# Input shape: parsed compose YAML, e.g.
#   {"services": {"<NAME>": {"image": ..., "build": ...}}}.

package main

deny[msg] {
	service := input.services[name]
	image := service.image
	endswith(image, ":latest")
	msg := sprintf("services.%v: image %q uses literal :latest — pin a concrete tag", [name, image])
}

deny[msg] {
	service := input.services[name]
	image := service.image
	# An image with no `:` has no tag at all → docker pulls :latest implicitly.
	not contains(image, ":")
	msg := sprintf("services.%v: image %q has no tag — append :<version>", [name, image])
}
