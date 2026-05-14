# Unit tests for compute.rego. Run via `conftest verify --policy policy/`.

package main

# --- POSITIVE: clean compute config produces no violations ---------------------

test_clean_compute_passes {
	# Clean fixture must satisfy every rule in package main (compute + tags),
	# since `deny` is a single set across all .rego files in the package.
	count(deny) == 0 with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"shape_config": {"ocpus": 2, "memory_in_gbs": 12},
		"source_details": {"boot_volume_size_in_gbs": 50},
		"freeform_tags": {"env": "prod", "component": "server"},
	}}}}
}

# --- NEGATIVE: each rule triggers on its specific violation --------------------

test_wrong_shape_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.E5.Flex",
		"shape_config": {"ocpus": 2, "memory_in_gbs": 12},
		"source_details": {"boot_volume_size_in_gbs": 50},
	}}}}
	contains(msg, "shape must be VM.Standard.A1.Flex")
}

test_too_many_ocpus_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"shape_config": {"ocpus": 5, "memory_in_gbs": 24},
		"source_details": {"boot_volume_size_in_gbs": 50},
	}}}}
	contains(msg, "exceeds Always Free A1.Flex cap of 4")
}

test_too_much_memory_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"shape_config": {"ocpus": 4, "memory_in_gbs": 25},
		"source_details": {"boot_volume_size_in_gbs": 50},
	}}}}
	contains(msg, "exceeds Always Free A1.Flex cap of 24 GB")
}

test_boot_volume_too_large_denied {
	deny[msg] with input as {"resource": {"oci_core_instance": {"opengate": {
		"shape": "VM.Standard.A1.Flex",
		"shape_config": {"ocpus": 2, "memory_in_gbs": 12},
		"source_details": {"boot_volume_size_in_gbs": 250},
	}}}}
	contains(msg, "exceeds Always Free cap of 200 GB")
}
