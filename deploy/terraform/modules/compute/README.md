# Compute submodule

Owns the OpenGate compute instance and the data sources required to construct it: availability-domain enumeration and the latest Ubuntu 24.04 image for the requested shape. Consumes networking outputs (`subnet_id`, `nsg_id`) from the [networking](../networking/) sibling.

## Inputs

| Variable | Type | Purpose |
|---|---|---|
| `compartment_id` | string (sensitive) | OCI compartment OCID that owns the instance. |
| `tenancy_ocid` | string (sensitive) | OCI tenancy OCID — required as `compartment_id` for the AD lookup, which spans the tenancy. |
| `subnet_id` | string | Public subnet from the networking module. |
| `nsg_ids` | list(string) | NSGs attached to the primary VNIC. Production passes `[networking.nsg_id]` so `cd.yml` can inject just-in-time SSH rules. |
| `instance_shape` | string | OCI compute shape — must remain `VM.Standard.A1.Flex` to qualify as Always Free. |
| `instance_ocpus` | number | OCPUs for the shape. Always Free cap is 4 OCPUs total per tenancy. |
| `instance_memory_gb` | number | Memory in GB. Always Free cap is 24 GB total per tenancy. |
| `boot_volume_gb` | number | Boot volume size in GB. Always Free cap is 200 GB total per tenancy. |
| `ssh_public_key_path` | string | Filesystem path read with `file(pathexpand(...))` into `metadata.ssh_authorized_keys`. |
| `cloud_init_path` | string | Filesystem path to the cloud-init YAML, base64-encoded into `metadata.user_data`. |

## Outputs

| Output | Purpose |
|---|---|
| `instance_id` | OCID of the compute instance (sensitive). |
| `public_ip` | Public IP of the compute instance — re-exported by the root as `instance_public_ip`. |

## Lifecycle

`metadata` and `source_details[0].source_id` are in `ignore_changes` so the instance does not redeploy on image refresh (cloud-init runs once on first boot) or on metadata edits applied out-of-band.

## Test coverage

- [`tests/free_tier.tftest.hcl`](tests/free_tier.tftest.hcl) — Always Free shape and sizing limits (shape pinned to A1.Flex, ≤4 OCPUs, ≤24 GB memory, ≤200 GB boot volume). Runs against a mock provider — no OCI creds required.
