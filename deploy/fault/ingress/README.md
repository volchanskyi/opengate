# Ingress fault profiles (edge, staging-only)

Version-controlled fault templates for reproducing edge 502/504 at ingress-nginx
on the **staging** host. Applied and reverted out-of-band by
[`scripts/fault/ingress-apply.sh`](../../../scripts/fault/ingress-apply.sh) and
[`scripts/fault/ingress-restore.sh`](../../../scripts/fault/ingress-restore.sh);
never part of the Helm chart. The mechanism decision lives in
[`docs/Fault-Injection.md`](../../../docs/Fault-Injection.md).

## Scenarios

| Scenario | Mechanism | Files |
|---|---|---|
| `edge-504` | Shrinks the staging Ingress `proxy-read`/`proxy-send` timeout so a backend delay (Chaos Mesh, FI5) exceeds it and nginx returns `504`. | [`edge-504-timeout.json`](./edge-504-timeout.json) merge patch |
| `edge-502` | Scales the staging server Deployment to zero so the upstream is unavailable and nginx returns `502`. No annotation — the apply script performs the scale. | — |

`502` is produced by upstream unavailability, **not** a reviewed critical-risk
nginx configuration snippet (the controller runs with snippet annotations
disabled — see [`../../helm/opengate/templates/ingress.yaml`](../../helm/opengate/templates/ingress.yaml)).

## Guarantees

- **Staging-only.** Both scripts refuse any namespace other than
  `opengate-staging`. The chart never embeds a `fault.opengate.dev/…`
  annotation, enforced at render time by
  [`policy/k8s/fault_injection.rego`](../../../policy/k8s/fault_injection.rego).
- **Reversible.** Apply snapshots the touched fields; restore returns the
  Ingress to a byte-identical state and is idempotent (safe to re-run from a
  cleanup `trap`).

## Usage

```sh
# In the staging cluster context:
scripts/fault/ingress-apply.sh   edge-504     # induce the fault
scripts/fault/ingress-restore.sh edge-504     # revert (also run from a trap)
```
