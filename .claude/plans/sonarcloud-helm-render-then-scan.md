# SonarCloud coverage of Helm charts — render-then-scan

## Context

Pointing `sonar.sources` at the raw Helm templates (`deploy/helm`) was tried on
2026-06-10 and reverted: SonarCloud's Kubernetes analyzer reads the raw `{{ }}`
templates, so ~17/20 findings were **template-resolution false-positives** it
can't resolve — `resources: {{- toYaml .Values.X.resources }}` reads as "no
limits" (S6870/S6897), and `serviceAccountName: {{ $name }}` can't be matched to
its `{{ $name }}` ClusterRoleBinding (S6865). Only ~3 findings were real, and all
legitimate (node-exporter `hostPID`, in-cluster `http://`, an example-file
placeholder). It is also **redundant**: the gauntlet already runs Checkov +
conftest/`policy/k8s` over the charts (`make lint-k8s`), which are purpose-built
for k8s/IaC.

To get *real* SonarCloud k8s findings on Helm, scan the **rendered** output (where
names + `toYaml` blocks are concrete), not the templates.

## Open question to resolve FIRST (before building)

**Is this worth it over the existing Checkov + conftest coverage?** Enumerate the
SonarCloud k8s rules that would add *net-new* signal beyond what Checkov +
`policy/k8s` already enforce. If the marginal value is small, the right answer is
**don't add it** — close this plan and rely on Checkov/conftest. Only build the
below if Sonar contributes rules the others don't.

## Approach (if it clears the value bar)

1. Add a `helm template` render step (Makefile target, e.g. `make sonar-helm-render`)
   that renders every chart with representative values — `deploy/helm/opengate`
   (with `values-production.yaml`, `values-staging.yaml`) and `deploy/helm/monitoring`
   — into a generated dir (e.g. `build/helm-rendered/`, gitignored).
2. Wire the rendered dir into the scan: either a dedicated SonarCloud analysis or a
   `sonar.sources` addition pointing at `build/helm-rendered/`. Generate it in the
   gauntlet **before** `make sonar` so the scanner sees it.
3. **Mapping wrinkle (must solve):** Sonar reports issues on the *rendered* file
   paths, which are generated/ephemeral, not tracked source. Decide how operators
   map a finding back to the source template (doc the convention, or a comment
   header in each rendered file pointing at its chart/template). Without this, a
   finding isn't actionable.
4. Handle the genuinely real findings in the templates (resource limits already
   set via values — confirm they render; node-exporter `hostPID` + in-cluster
   `http` reviewed-as-safe with justification).

## Critical files
- `sonar-project.properties` (rendered-dir source or a separate scanner run)
- `Makefile` (render target, sequenced before `make sonar`)
- `.gitignore` (the rendered output dir)
- `scripts/precommit-gauntlet.sh` (render before scan)

## Verification
- A full `make sonar` (NEVER `sonar-quick`) shows the rendered helm analyzed with
  the template-FPs gone (resources/names resolved); remaining findings are real.
- Gate green; the few legitimate accepts are justified, not blanket-suppressed.
