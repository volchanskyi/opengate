# Module: `oke`

Provisions the **Oracle Kubernetes Engine (OKE)** cluster + Always-Free A1.Flex
node pool that hosts the [Helm-packaged stack](../../../helm/opengate/). See
[ADR-030](../../../../docs/adr/ADR-030-kubernetes-adoption-oke-helm.md) for the
platform decisions and
[docs/Kubernetes-Migration.md](../../../../docs/Kubernetes-Migration.md) for the
one-time cutover.

## Not wired into the root stack (yet)

The root `deploy/terraform` config still manages the single-VM compose
deployment. This module is applied **separately at cutover** (so a routine root
`plan` never tries to create a cluster). Wiring it into the root `main.tf` —
together with the OKE-compliant subnets/NSGs and the `oci-bv` CSI default
StorageClass — is the cutover step in the migration runbook.

## Inputs

Networking inputs (`vcn_id`, `*_subnet_id`, `*_nsg_ids`, `availability_domain`)
come from the networking module / root. `kubernetes_version` and `node_image_id`
are resolved from OCI:

```sh
oci ce cluster-options get --cluster-option-id all          # kubernetes versions
oci ce node-pool-options get --node-pool-option-id all      # node images
```

The full variable surface (with defaults and the Always-Free caps each one is
bounded by) is documented inline in [`variables.tf`](./variables.tf).

## Free-tier invariants

[`tests/free_tier.tftest.hcl`](./tests/free_tier.tftest.hcl) (run via
`make terraform-test`) stamps the Always-Free guardrails at plan time:

- control plane stays `BASIC_CLUSTER` (free)
- node shape stays `VM.Standard.A1.Flex`
- `node_ocpus × node_pool_size ≤ 4` OCPUs and `node_memory_gb × node_pool_size ≤ 24` GB
- `node_boot_volume_gb × node_pool_size ≤ 200` GB block storage
- Kubernetes dashboard add-on disabled
- `node_pool_size` variable validation rejects a pool that would blow the OCPU cap

## Outputs

`cluster_id` (feed to `oci ce cluster create-kubeconfig`), `cluster_name`,
`node_pool_id`, `kubernetes_version`.
