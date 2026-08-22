# OpenGate-specific Always-Free compute invariants for the OCI provider.
# Overlaps with deploy/terraform/modules/compute/tests/free_tier.tftest.hcl —
# deliberate defense-in-depth (different parser, different gate location).
#
# Input shape: HCL2 parser output from conftest, i.e.
#   {"resource": {"oci_core_instance": {"<NAME>": {<attrs>}}}}
# `shape_config` and other repeated blocks are NESTED OBJECTS in this view,
# not lists (different from `terraform show -json plan`).
#
# Reference: https://docs.oracle.com/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm

package main

# DENY: instance shape must remain VM.Standard.A1.Flex (Always Free ARM64).
deny[msg] {
	instance := input.resource.oci_core_instance[name]
	instance.shape != "VM.Standard.A1.Flex"
	msg := sprintf("oci_core_instance.%v: shape must be VM.Standard.A1.Flex (Always Free ARM64); got %q", [name, instance.shape])
}

# The A1.Flex ceiling is held equal to the Terraform guards in
# deploy/terraform/modules/{compute,oke}/tests/free_tier.tftest.hcl. Two gates on
# the same grant that disagree are worse than one: the looser admits a plan the
# stricter refuses, and which one runs decides whether the plan lands. The
# figures below are the stricter pair, so agreement is reached without widening
# what either gate allows.
free_a1_ocpus := 2

free_a1_memory_gbs := 12

# DENY: shape_config.ocpus must stay within the Always Free A1.Flex tenant cap.
deny[msg] {
	instance := input.resource.oci_core_instance[name]
	cfg := instance.shape_config
	cfg.ocpus > free_a1_ocpus
	msg := sprintf("oci_core_instance.%v: shape_config.ocpus=%v exceeds Always Free A1.Flex cap of %v", [name, cfg.ocpus, free_a1_ocpus])
}

# DENY: shape_config.memory_in_gbs must stay within the Always Free A1.Flex tenant cap.
deny[msg] {
	instance := input.resource.oci_core_instance[name]
	cfg := instance.shape_config
	cfg.memory_in_gbs > free_a1_memory_gbs
	msg := sprintf("oci_core_instance.%v: shape_config.memory_in_gbs=%v exceeds Always Free A1.Flex cap of %v GB", [name, cfg.memory_in_gbs, free_a1_memory_gbs])
}

# DENY: boot volume must be <= 200 GB (Always Free per-tenancy cap).
deny[msg] {
	instance := input.resource.oci_core_instance[name]
	src := instance.source_details
	src.boot_volume_size_in_gbs > 200
	msg := sprintf("oci_core_instance.%v: source_details.boot_volume_size_in_gbs=%v exceeds Always Free cap of 200 GB", [name, src.boot_volume_size_in_gbs])
}
