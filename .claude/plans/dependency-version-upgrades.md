# Dependency holes only a newer minor or major fixes

Ivan's decision (2026-10-23): the advisories the dependency check lists as fixed only in a newer
minor or major release are gathered here, grouped by dependency, instead of being raised one at a
time. They never fail the check. Nothing below moves without Ivan's explicit approval of that one
move ([`tool-versions.md`](../rules/tool-versions.md)): each row is a question to bring to him, with
what the move changes and what it risks, researched before it is asked. Every row was asked and
decided on 2026-11-01 (below). Split out of [`dependency-newer-lines.md`](dependency-newer-lines.md)
the same day (Ivan).

Measured by the full check on 2026-11-01 (459 advisories), after every patch move was taken. A fresh
list comes from `python3 scripts/dependency-fixes`, under "fixed only in a newer minor or major".

"Major" follows the rule's definition: the first non-zero number changes, so `0.72` → `0.76` is one.

## What customers' traffic runs through

| Dependency | In use | Release that drops the holes | Move | Holes | Examples | Declared in |
|---|---|---|---|---|---|---|
| golang.org/x/net (the server's module graph; found 2026-11-01) | 0.61.0 | 0.62.0 | major | 2 | GO-2026-6860, GO-2026-6866 | `server/go.mod` |
| alpine (the server image's base) | 3.20, read at its newest build | 3.24 | minor | 1 | CVE-2026-107891 | `Dockerfile` |
| grafana/grafana | 11.6.16 | 13.3.0 | major | 121 | CVE-2025-64751 | `deploy/helm/monitoring/values.yaml` |
| victoriametrics/victoria-metrics | v1.114.0 | v1.155.0 | minor | 129 | CVE-2024-58251 | monitoring values, compose files, CI, Makefile, `testvm.go`, `test-go.sh` |
| grafana/loki | 3.5.12 | 3.7.9 | minor | 81 | CVE-2026-111943 | monitoring values |
| prom/node-exporter | v1.9.1 | v1.12.1 | minor | 74 | CVE-2025-0913 | monitoring values |
| curlimages/curl | 8.11.1 | 8.23.0 | minor | 65 | CVE-2024-12797 | `deploy/helm/opengate/values.yaml`, `network-drill.yml`, `vm-push.sh`, `vm-query.sh` |
| grafana/promtail | 3.5.12 | 3.6.11 | minor | 19 | CVE-2026-21726 | monitoring values |

## What builds, tests and deploys the product

| Dependency | In use | Release that drops the holes | Move | Holes | Examples |
|---|---|---|---|---|---|
| node (the server image's web build stage; found 2026-10-27) | 24-alpine, read at its newest build | 25-alpine | major | 9 | CVE-2026-101910 |
| Playwright browser image | v1.63.0-noble | v1.64.1-noble (follows `@playwright/test` in the web lockfile) | minor | 130 | CVE-2024-2236 |
| Terraform CLI | 1.9.8 | v1.17.1 | minor | 73 | GHSA-65xw-vw82-r86x, GHSA-xmrv-pmrh-hhx2 |
| sonar-scanner-cli image | 12.2.0.4256_8.1.0 | 12.3.0.4285_8.1.0 | minor | 71 | CVE-2025-8869 |
| Helm | 3.20.2 | v3.22.1 | minor | 69 | GHSA-7jxh-36q5-gcqv, GHSA-fqw6-gf59-qr4w |
| conftest | 0.55.0 | v0.71.1 | major | 64 | GHSA-92mm-2pjq-r785, GHSA-wjrx-6529-hcj3 |
| Trivy | 0.72.0 | v0.76.1 | major | 50 | GHSA-p7v4-vr35-mj6f, GHSA-pg57-6jwg-q645 |
| k6 | v1.6.1 | v1.8.2 | minor | 47 | GHSA-mh2q-q3fh-2475, GHSA-w8rr-5gcm-pp58 |
| Terraform provider oracle/oci | 6.37.0 | 9.11.0 | major | 42 | GHSA-2x5j-vhc8-9cwm, GHSA-q9hv-hpm4-hj6x |
| jq | 1.7.1 | 1.8.2 | minor | 29 | CVE-2024-23337, CVE-2025-48060 |
| pmat | 3.17.0 | 3.42.0 | minor | 25 | RUSTSEC-2026-0190, RUSTSEC-2026-0204 |
| tflint | 0.64.0 | v0.65.0 | major | 17 | GO-2026-6303 |
| semgrep (what its environment holds) | 1.108.0 | 1.183.0 and a fresh venv (setuptools, urllib3, certifi are the install's); opentelemetry-api 1.45.1 no release admits | minor | 11 | GHSA-6q48-6c7w-9gw2, GHSA-xcq7-p3pf-p88h |
| cargo-modules | 0.26.0 | 0.28.0 | major | 4 | RUSTSEC-2026-0308 |
| cargo-deny | 0.19.6 | 0.20.2 | major | 3 | RUSTSEC-2026-0186 |
| oapi-codegen | 2.7.2 | v2.8.2 | minor | 3 | GHSA-mmfr-pmjx-hw9w, GO-2026-5970 |
| minimatch (three copies in the web lockfile: 3.1.5, 5.1.9, 10.2.6; found 2026-11-01) | as listed | 11.0.1 | major | 3 | GHSA-g3hv-75wg-m249 |
| cargo-llvm-cov | 0.8.5 | 0.9.1 | major | 2 | GHSA-3pv8-6f4r-ffg2 |
| gitleaks | 8.21.4 | v8.30.1 | minor | 2 | GHSA-hcg3-q754-cr77 |
| Lighthouse CI | 0.14.0 | 0.15.1 | major | 1 | GHSA-pxg6-pf52-xh8x |
| oci-cli (what its environment holds) | 3.94.1 | none yet: every release to 3.95.1 requires certifi below 2026 | kept | 1 | GHSA-6q48-6c7w-9gw2 |
| shfmt | 3.13.1 | v3.14.1 | minor | 1 | GO-2026-5024 |

Decided (Ivan, 2026-11-01), each measured with the check's own package reader against the tags
named (every one built for arm64; the production arm64 check still runs before deploy):

- golang.org/x/net 0.62.0 — nothing else in the graph moves (same Go, crypto, sys, term, text); the
  release also adds bounds checks in `ipv4`/`ipv6`, which quic-go reads the server's socket through.
- alpine 3.24 — 3.20 left support 2026-04-01; 3.24 is supported to 2028-06. The hole is in musl,
  which the static server never calls. 3.24 carries no fixable finding.
- Grafana 12.4.14 — 11.6 left support 2026-06-25. 12.4.14 keeps 7 of 213 findings (13.3.0 keeps 23);
  12.4 is supported to 2027-05-24. Data-source UIDs `VictoriaMetrics` and `Loki` meet 12.0's format;
  no Angular panels.
- VictoriaMetrics v1.155.0 — keeps 2 of 187; the free path is the newest release (the LTS lines are
  enterprise builds). Every declaration moves together.
- Loki 3.7.9 — 3.5 is unpatched; 3.7.9 keeps 2 of 90 (3 in all). Monolithic, filesystem, tsdb v13 as
  today.
- node-exporter v1.12.1 — keeps 32 of 107 (the newest release); our one flag is unchanged.
- curl 8.23.0 — keeps 0 of 120 (88 were OpenSSL, on the backup upload's path).
- **Promtail → Grafana Alloy v1.21.3** (an end-of-life replacement): Promtail ended 2026-03-02 and
  3.6.11 keeps 254 of 283; Alloy carries 3. Test first: the monitoring chart's Promtail
  DaemonSet becomes Alloy from the converted config (`alloy convert --source-format=promtail`), same
  pod-log paths, labels and OKE root rule; its memory read back against the 256 Mi limit; the
  config, cluster permissions, `.checkov.yaml`, the three shell tests, `NOTES.txt`,
  [`Monitoring.md`](../../docs/infrastructure/Monitoring.md) and the audit skills name Alloy.

Build, test and deploy rows, decided the same day (each move proven by the step that runs the tool):

- Playwright image v1.64.1-noble, with `@playwright/test` 1.64.1 in the web lockfile.
- Terraform 1.17.1 — `terraform test` and a plan against the live state show no change.
- sonar-scanner-cli 12.3.0.4285_8.1.0.
- Helm v3.22.1 (Helm 4 is not part of this move).
- **conftest v0.71.1, policies rewritten to OPA 1.0 syntax** — test first: the ten files under
  `policy/` converted with `opa fmt --v0-v1`; `conftest verify` and every `conftest test` reach the
  same verdicts before and after; no `--rego-version` switch.
- Trivy v0.76.1 (no custom report templates use the removed function).
- k6 v1.8.2.
- oracle/oci provider 9.11.0 — `~> 9.0` in all six files and the lock file; `terraform test` and a
  live plan show no change (7.0–9.0 remove only services not in use; 8.29.0 keeps x/crypto's holes).
- jq 1.8.2 — every shell test and workflow sweep passes; a script whose output changes is fixed in
  the same commit.
- **pmat 3.42.0** — the new token-walk grading reads the tree first; every file it puts below B+ is
  split by concern in the same commit, and the repo floor is set to its reading (below).
- tflint v0.65.0.
- **semgrep 1.183.0, install fixed** — test first: `install-semgrep.sh` upgrades setuptools within
  its cap, the cap goes if 1.183.0 no longer imports `pkg_resources`, the venv is rebuilt fresh, and
  the pen-test fixtures give identical verdicts. opentelemetry-api stays listed until semgrep admits
  1.45.1.
- cargo-modules 0.28.0 (minimum Rust 1.98; 1.99.1 in use) — the snapshot diff holds or is
  regenerated.
- cargo-deny 0.20.2 (the one call, `check --hide-inclusion-graph`, is untouched by its CLI change).
- **oapi-codegen v2.8.2 with `compatibility: enable-auth-scopes-on-context: true`**, so
  `BearerAuthScopes`/`CookieAuthScopes` keep reaching the auth check in `api.go`; the regenerated
  file differs only where 2.8 changes output, and the auth tests and pen-test gate pass.
- cargo-llvm-cov 0.9.1.
- gitleaks v8.30.1 — a full-history scan first; a new finding is removed, or brought to Ivan before
  any allowlist entry.
- Lighthouse CI 0.15.1 — budgets stay; one the new Lighthouse misses comes to Ivan with both readings.
- shfmt v3.14.1 — `make shell-fmt` applies its changes in the same commit.

Kept, each listed by the check until the line in use can take the fix. Recorded in one Low register
entry (Ivan, 2026-11-01), written in this plan's commit, naming the three and what clears each:

- node 24-alpine (the web build stage): all 19 findings are npm's bundled libraries, never shipped;
  25-alpine is out of support with 83, 26-alpine carries the same 19, and CI uses Node 24.
- minimatch: every copy comes from a dev tool (eslint, typescript-eslint, Stryker,
  openapi-typescript, ts-prune); 11.0 removes `makeRe()`, so the copies move when their owners do.
- oci-cli 3.94.1: every release to 3.95.1 requires certifi below 2026, so none takes the fix.

## When it lands, and what it waits on

- After commit 3 of [`dependency-newer-lines.md`](dependency-newer-lines.md). Three moves change
  what that plan measures: k6 generates the technician load, VictoriaMetrics runs in the performance
  stack (`deploy/docker-compose.perf.yml`), and alpine is the base of the server image that stack
  builds. The one-runner measurement and the nights that set the spike limit run on today's
  versions of all three, so the limit is set and enforced on one stack.
- The moves that belong together stay together: the Playwright image and `@playwright/test`;
  Terraform and the oracle/oci provider; Trivy and the dependency check that runs it; pmat and the
  repo-score floor.
- **pmat and the repo floor:** the repo-score floor (64.0, set by item 3 of the other plan) follows
  pmat 3.42.0's reading of the same tree, set in the commit that moves pmat.
- As few commits as the waits allow, whatever their size (Ivan, 2026-10-25), each test first where
  a row above says so. The commit that lands the last move deletes this plan and adds its
  [`phases.md`](../phases.md) row.
