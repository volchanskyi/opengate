# Dependency holes only a newer minor or major fixes

Ivan's decision (2026-10-23): the advisories the dependency check lists as fixed only in a newer
minor or major release are gathered here, grouped by dependency, instead of being raised one at a
time. They never fail the check. Nothing below moves without Ivan's explicit approval of that one
move ([`tool-versions.md`](../rules/tool-versions.md)): each row is a question to bring to him, with
what the move changes and what it risks, researched before it is asked.

Measured by the full check on 2026-10-23 (458 advisories), after every patch move was taken. A fresh
list comes from `python3 scripts/dependency-fixes`, under "fixed only in a newer minor or major".

"Major" follows the rule's definition: the first non-zero number changes, so `0.72` → `0.76` is one.

## What customers' traffic runs through

| Dependency | In use | Release that drops the holes | Move | Holes | Examples | Declared in |
|---|---|---|---|---|---|---|
| alpine (the server image's base) | 3.20, read at its newest build | 3.24 | minor | 1 | CVE-2026-107891 | `Dockerfile` |
| grafana/grafana | 11.6.16 | 13.2.4 | major | 125 | CVE-2025-64751 | `deploy/helm/monitoring/values.yaml` |
| victoriametrics/victoria-metrics | v1.114.0 | v1.154.1 | minor | 126 | CVE-2024-58251 | monitoring values, compose files, CI, Makefile, `testvm.go`, `test-go.sh` |
| grafana/loki | 3.5.12 | 3.7.9 | minor | 81 | CVE-2026-111943 | monitoring values |
| prom/node-exporter | v1.9.1 | v1.12.1 | minor | 74 | CVE-2025-0913 | monitoring values |
| curlimages/curl | 8.11.1 | 8.23.0 | minor | 64 | CVE-2024-12797 | `deploy/helm/opengate/values.yaml`, `network-drill.yml`, `vm-push.sh`, `vm-query.sh` |
| cert-manager (chart, program and its four images) | v1.20.4 | v1.21.3 | minor | 88 | GHSA-8wmf-6v46-5gfg, GO-2026-6354, CVE-2026-111943 | `TOOL_VERSION_CERT_MANAGER_CHART` |
| grafana/promtail | 3.5.12 | 3.6.11 | minor | 19 | CVE-2026-21726 | monitoring values |

## What builds, tests and deploys the product

| Dependency | In use | Release that drops the holes | Move | Holes | Examples |
|---|---|---|---|---|---|
| Playwright browser image | v1.63.0-noble | v1.64.1-noble (follows `@playwright/test` in the web lockfile) | minor | 130 | CVE-2024-2236 |
| Terraform CLI | 1.9.8 | v1.17.0 | minor | 71 | GHSA-65xw-vw82-r86x, GHSA-xmrv-pmrh-hhx2 |
| Helm | 3.20.2 | v3.22.1 | minor | 69 | GHSA-7jxh-36q5-gcqv, GHSA-fqw6-gf59-qr4w |
| conftest | 0.55.0 | v0.71.1 | major | 64 | GHSA-92mm-2pjq-r785, GHSA-wjrx-6529-hcj3 |
| sonar-scanner-cli image | 12.2.0.4256_8.1.0 | 12.3.0.4285_8.1.0 | minor | 51 | CVE-2025-8869 |
| Trivy | 0.72.0 | v0.76.1 | major | 48 | GHSA-p7v4-vr35-mj6f, GHSA-pg57-6jwg-q645 |
| k6 | v1.6.1 | v1.8.2 | minor | 47 | GHSA-mh2q-q3fh-2475, GHSA-w8rr-5gcm-pp58 |
| Terraform provider oracle/oci | 6.37.0 | 9.11.0 | major | 42 | GHSA-2x5j-vhc8-9cwm, GHSA-q9hv-hpm4-hj6x |
| jq | 1.7.1 | 1.8.2 | minor | 29 | CVE-2024-23337, CVE-2025-48060 |
| pmat | 3.17.0 | 3.42.0 | minor | 21 | RUSTSEC-2026-0190, RUSTSEC-2026-0204 |
| tflint | 0.64.0 | v0.65.0 | major | 17 | GO-2026-6303 |
| semgrep (what its environment holds) | 1.108.0 | a newer semgrep that admits setuptools 84.1.0, urllib3 2.8.0, protobuf 5.29.6, certifi 2026.10.19, opentelemetry-api 1.45.1 | to research | 11 | GHSA-6q48-6c7w-9gw2, GHSA-xcq7-p3pf-p88h |
| cargo-deny | 0.19.6 | 0.20.2 | major | 3 | RUSTSEC-2026-0186 |
| cargo-modules | 0.26.0 | 0.28.0 | major | 3 | RUSTSEC-2026-0308 |
| oapi-codegen | 2.7.2 | v2.8.1 | minor | 3 | GHSA-mmfr-pmjx-hw9w, GO-2026-5970 |
| cargo-llvm-cov | 0.8.5 | 0.9.1 | major | 2 | GHSA-3pv8-6f4r-ffg2 |
| gitleaks | 8.21.4 | v8.30.1 | minor | 2 | GHSA-hcg3-q754-cr77 |
| Lighthouse CI | 0.14.0 | 0.15.1 | major | 1 | GHSA-pxg6-pf52-xh8x |
| oci-cli (what its environment holds) | 3.94.1 | a newer oci-cli that admits certifi 2026.10.19 | to research | 1 | GHSA-6q48-6c7w-9gw2 |
| shfmt | 3.13.1 | v3.14.1 | minor | 1 | GO-2026-5024 |

## How to work it

1. Order by what customers' traffic runs through first, then by holes per move.
2. For each row: read the advisories, the release notes between the two versions, and how
   comparable products took the same move; then bring Ivan one question with options (take it,
   keep the version in use), saying what changes and what it risks, with an RMM example.
3. A move Ivan approves lands as its own change through the gauntlet; the check then shows the
   row gone.
