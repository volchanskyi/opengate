# Fault-injection manifest policy — runs against the rendered Helm output
# alongside security.rego:
#   helm template … | conftest test -p policy/k8s -
#
# Rule (deny):
#   Fault-injection annotations (any `fault.opengate.dev/…` key) are applied
#   out-of-band to the *staging* Ingress by scripts/fault/ingress-apply.sh for
#   the duration of a drill and removed afterwards. They must never be baked
#   into a chart template, which would render into every environment —
#   production included. This denies any rendered document carrying the marker,
#   so the production render provably cannot ship a fault annotation.
#
# Input shape: a single rendered Kubernetes manifest document. Documents without
# a fault annotation (every normal manifest) match no rule and pass.

package main

fault_annotation_prefix := "fault.opengate.dev/"

deny[msg] {
	some key
	input.metadata.annotations[key]
	startswith(key, fault_annotation_prefix)
	msg := sprintf("%v/%v: carries fault-injection annotation %q — fault annotations are staging-only and applied out-of-band, never in a chart template", [input.kind, input.metadata.name, key])
}
