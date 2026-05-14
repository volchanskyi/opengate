# Tagging invariants for OCI resources.
#
# Scope (deliberate): currently this rule fires on `oci_core_instance` only.
# The networking submodule (VCN, IGW, RT, NSG, SL, Subnet) is *not* tagged
# yet, and adding tags there requires another `terraform apply` to flush
# metadata into OCI. The rule will broaden to all taggable resources in a
# follow-up sweep PR; for now it codifies the floor we *enforce*: the compute
# instance — the only resource that materially affects billing and the
# resource the operator most often needs to disambiguate by env/component.

package main

# Required tag keys on the compute instance. Add new keys here as the project
# adopts them; the policy fails when any required key is missing.
required_instance_tags := {"env", "component"}

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	missing := required_instance_tags - object.keys(_freeform_tags(instance))
	count(missing) > 0
	msg := sprintf("oci_core_instance.%v: freeform_tags missing required key(s) %v", [name, missing])
}

# Helper — return freeform_tags or an empty object if absent.
_freeform_tags(r) = r.freeform_tags

_freeform_tags(r) = {} {
	not r.freeform_tags
}
