package main

test_instance_with_all_tags_passes {
	count(deny) == 0 with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"freeform_tags": {"env": "prod", "component": "server"},
	}}}}
}

test_instance_with_extra_tags_still_passes {
	# Required tags are a floor, not a ceiling — extras are fine.
	count(deny) == 0 with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"freeform_tags": {"env": "prod", "component": "server", "managed_by": "terraform"},
	}}}}
}

test_instance_missing_env_tag_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"freeform_tags": {"component": "server"},
	}}}}
	contains(msg, "missing required key(s)")
}

test_instance_with_no_tags_block_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
	}}}}
	contains(msg, "missing required key(s)")
}
