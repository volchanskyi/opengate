# The input is conftest's HCL2 parse, where shape_config and other repeated blocks are objects.

package main

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	instance.shape != "VM.Standard.A1.Flex"
	msg := sprintf("oci_core_instance.%v: shape must be VM.Standard.A1.Flex (Always Free ARM64); got %q", [name, instance.shape])
}

# The A1.Flex ceiling equals the figures in the Terraform free_tier tests, so both gates agree.
free_a1_ocpus := 2

free_a1_memory_gbs := 12

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	cfg := instance.shape_config
	cfg.ocpus > free_a1_ocpus
	msg := sprintf("oci_core_instance.%v: shape_config.ocpus=%v exceeds Always Free A1.Flex cap of %v", [name, cfg.ocpus, free_a1_ocpus])
}

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	cfg := instance.shape_config
	cfg.memory_in_gbs > free_a1_memory_gbs
	msg := sprintf("oci_core_instance.%v: shape_config.memory_in_gbs=%v exceeds Always Free A1.Flex cap of %v GB", [name, cfg.memory_in_gbs, free_a1_memory_gbs])
}

deny[msg] {
	instance := input.resource.oci_core_instance[name]
	src := instance.source_details
	src.boot_volume_size_in_gbs > 200
	msg := sprintf("oci_core_instance.%v: source_details.boot_volume_size_in_gbs=%v exceeds Always Free cap of 200 GB", [name, src.boot_volume_size_in_gbs])
}
