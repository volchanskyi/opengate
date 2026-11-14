# Module: `oke`

Provisions the **Oracle Kubernetes Engine (OKE)** cluster + Always-Free A1.Flex
node pool that hosts the [Helm-packaged stack](../../../helm/opengate/). The root
[`main.tf`](../../main.tf) calls it as `module "oke"`. See
[ADR-030](../../../../docs/adr/ADR-030-kubernetes-on-oke.md) for the
platform decisions.

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

## Node start-up

The node pool hands OKE [`node-init.sh`](./node-init.sh) as each node's start-up
script: it writes the kubelet's image clean-up drop-in, then runs OKE's own
start-up with the kubelet pointed at it. OKE runs it when it creates a node; a
running node takes a change by hand
([ADR-030](../../../../docs/adr/ADR-030-kubernetes-on-oke.md)).

## Plan-time invariants

[`tests/free_tier.tftest.hcl`](./tests/free_tier.tftest.hcl) (run via
`make terraform-test`) stamps these at plan time:

- control plane stays `BASIC_CLUSTER` (free)
- node shape stays `VM.Standard.A1.Flex`
- `node_ocpus × node_pool_size` and `node_memory_gb × node_pool_size` stay
  within the Always-Free compute cap
- `node_boot_volume_gb × node_pool_size` stays within the Always-Free block
  storage cap
- Kubernetes dashboard add-on disabled
- the node's start-up script writes the image clean-up drop-in and starts the
  kubelet reading it
- `node_pool_size` variable validation rejects a pool that would blow the OCPU cap

## Outputs

`cluster_id` (feed to `oci ce cluster create-kubeconfig`), `cluster_name`,
`node_pool_id`, `kubernetes_version`.
