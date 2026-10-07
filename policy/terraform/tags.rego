# The tag rule covers oci_core_instance, the resource that materially affects billing.

package main

required_instance_tags := {"env", "component"}

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	missing := required_instance_tags - object.keys(_freeform_tags(instance))
	count(missing) > 0
	msg := sprintf("oci_core_instance.%v: freeform_tags missing required key(s) %v", [name, missing])
}

_freeform_tags(r) = r.freeform_tags

_freeform_tags(r) = {} {
	not r.freeform_tags
}
