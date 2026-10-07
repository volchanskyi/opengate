# Fault annotations (fault.opengate.dev/ keys) are applied out-of-band to the staging Ingress.
# A rendered document carrying one is denied, so a chart template cannot ship it to production.

package main

fault_annotation_prefix := "fault.opengate.dev/"

deny[msg] {
	some key
	input.metadata.annotations[key]
	startswith(key, fault_annotation_prefix)
	msg := sprintf("%v/%v: carries fault-injection annotation %q — fault annotations are staging-only and applied out-of-band, never in a chart template", [input.kind, input.metadata.name, key])
}
