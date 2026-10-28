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
| alpine (the server image's base) | 3.20, read at its newest build | 3.24 | minor | 2 | CVE-2026-107891 | `Dockerfile` |
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
| node (the server image's web build stage; found 2026-10-27) | 24-alpine, read at its newest build | 25-alpine | major | 6 | CVE-2026-101910 |
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
3. A move Ivan approves rides in the next commit of [Commits](#commits-ivan-2026-10-25), at the
   latest commit 3; every row is asked before commit 3. The check then shows the row gone.

## Also in this plan: staging drills skipped ahead of production

Ivan's decision (2026-10-24): this fix rides in this plan. It moves no dependency and is commit 1
(Ivan, 2026-10-25), with the CI/CD rule split below (Ivan, 2026-10-26).

### What is wrong

- `staging-fault-drills` in [`cd.yml`](../../.github/workflows/cd.yml) has no status function in
  its `if:`, so GitHub applies `success()`, which reads every job upstream, not only its `needs`.
- `deploy-staging-k8s` needs the `cluster` job, which runs only when the cluster-wide pieces
  changed (since 615f1d28, 2026-10-22). A skipped `cluster` therefore skips the drills, even with
  staging deployed and `STAGING_FAULT_TESTS` set to `true`.
- `deploy-production-k8s` accepts a `skipped` drill job as a pass, so production promotes with no
  drill. Run 39936850693 (2026-10-23) did; run 40136246809 (2026-10-24) waits on approval to do
  the same.
- Every CD run since 615f1d28 shows it: drills ran only in 40125081581, the one run where `cluster`
  ran and staging deployed.
- [`fault-ci.test.sh`](../../scripts/tests/fault-ci.test.sh) asserts only that the string
  `STAGING_FAULT_TESTS` appears in `cd.yml`, so nothing caught it.
- `deploy-staging-k8s` and `deploy-production-k8s` state `always()`, and GitHub keeps a job whose
  condition holds `always()` running after a cancel (run 39913420184: its `always()` alert job
  started 3 s after the cancel). A cancel during a staging deploy does not stop it; production
  still waits for a reviewer, since both environments require one.

### The fix

1. Test first: a sweep in [`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh),
   reading workflows as structure, that fails a job with no status function in its `if:` whose
   upstream chain (`needs`, transitively) holds a job with an `if:` of its own, so it can be
   skipped; and a job-level `always()` on a job that neither sends an alert nor cleans up, since
   that job outlives a cancel. It demonstrates both defects first against fixtures with these
   shapes, counts what it reached, and fails against today's `cd.yml`.
2. `staging-fault-drills`: `!cancelled() && needs.deploy-staging-k8s.result == 'success' &&` the
   existing two conditions.
3. `deploy-production-k8s` and `deploy-staging-k8s`: `!cancelled()` replaces `always()`, keeping
   each explicit result check. In production, a `skipped` drill job passes only when
   `vars.STAGING_FAULT_TESTS != 'true'`; with drills switched on, only `success` promotes.
4. Fix every other job the new sweep names, in the same change: of the 27 job-level `always()`
   across 13 workflows (2026-10-25), alert and clean-up jobs keep it, since an alert reports a
   cancellation.
5. Rule: a section in [`ci-cd-determinism.md`](../rules/ci-cd-determinism.md), "A job below a
   skippable job states its own status, and only an alert or clean-up job outlives a cancel",
   naming the sweep.
6. Docs: [`Fault-Injection.md`](../../docs/infrastructure/Fault-Injection.md) states that a drill
   skipped while `STAGING_FAULT_TESTS` is `true` blocks promotion.

## Also in this plan: the CI/CD rule split

Ivan's decision (2026-10-26): it rides in commit 1, beside the staging-drill fix, so that fix's
new section lands in the new structure.

### What is wrong

- [`ci-cd-determinism.md`](../rules/ci-cd-determinism.md) is the largest rule: 185 lines, 16
  sections, six topics (caches and artifacts, what a workflow hands a script, verdicts, alerting
  and monitoring, environments, the mutation budget). It grew from 146 to 185 lines between
  2026-09-23 and 2026-10-24.
- The rule and its checks disagree. Artifacts: every artifact a later job reads is claimed, the
  three `mutation.yml` uploads are checked, and 19 of 35 uploads leave `if-no-files-found` unset.
  "An exemption is re-earned" has no check. The absence-check section has no sweep, only
  `smoke-test.sh`'s own code. The guard checks that 35 nightly cluster calls are retried or marked
  `Not retried`, which no section states.
- Four sections are held by other guards: the two alert sections and the monitoring section
  (`alert-delivery.test.sh`, `monitoring-config-apply.test.sh`), and the mutation budget
  (`mutation-workflow.test.sh`).
- Rationale sits in the rule, though the ADR is its only home, and "What the sweeps do" repeats
  the guard conventions [`assertion-determinism.md`](../rules/assertion-determinism.md) states.
- [`ci-cd-determinism.test.sh`](../../scripts/tests/ci-cd-determinism.test.sh) takes 16.1 s, 13.7 s
  of it in the required-inputs sweep, which re-reads every workflow once per script (11 479 `grep`
  starts). Jobs and workflow-level env are sliced out as text with `awk`.
- Kept, by measurement: the pasted-input sweep. The pinned `actionlint` flags
  `github.event.pull_request.title` and `github.head_ref` in a `run:` block, but not `inputs.*`
  or `github.event.before` (tested 2026-10-26).

### The fix

1. Test first: the binding check (step 5) fails against today's rules, naming each mismatch.
2. Split by topic:
   - `ci-cd-determinism.md`, "A refused step is not a green one": cache read-back; no cache
     declared where the token cannot write; the `mutation.yml` uploads fail on an empty file set;
     the staging target check; a status read with errexit off; environments only where a person
     approves; each nightly cluster call retried or marked `Not retried`; and the staging-drill
     section above.
   - `workflow-inputs.md`, new: an input a script refuses to run without is named in the job that
     calls it; an input reaches a script as a value; a read is spelled as a read (`gh api`).
   - [`tool-versions.md`](../rules/tool-versions.md) gains `cargo install --locked`, beside the
     other install rules.
   - The three alert sections (an alert conditioned on a healthy run, a refused send, a
     configuration the cluster was never given) leave the rules for
     [ADR-123](../../docs/adr/ADR-123-alert-delivery.md), with no rule of their own (Ivan,
     2026-10-26: ADR only); `alert-delivery.test.sh`, `monitoring-config-apply.test.sh` and
     `telegram-alert.test.sh` keep holding them.
   - The mutation budget leaves the rules for
     [ADR-097](../../docs/adr/ADR-097-mutation-budget.md), the budget's own ADR;
     `mutation-workflow.test.sh` keeps holding it.
   - Each section claims only what is checked (Ivan chose narrowing over fixing the 19 uploads,
     2026-10-26): the artifact section names `mutation.yml`, and the two sections with no check,
     "An exemption is re-earned" and "A check that asserts an absence proves it reached something
     first", are removed. `smoke-test.sh` keeps asking `edge_answered` first.
3. Rationale moves to ADRs, and each rule keeps the instruction and one example: caches to
   [ADR-086](../../docs/adr/ADR-086-deploy-reads-the-cluster.md), the staging check and
   environments to [ADR-084](../../docs/adr/ADR-084-staging-environment.md) and
   [ADR-107](../../docs/adr/ADR-107-where-a-run-happens.md), alerts and monitoring to
   [ADR-123](../../docs/adr/ADR-123-alert-delivery.md), and what no ADR covers to a new ADR each:
   the verdict sections, and the workflow inputs.
4. How a guard is written — show the defect first, read the subject whole, count what was read,
   fail on a sweep that read nothing, print its check's ID — moves to
   [ADR-029](../../docs/adr/ADR-029-test-determinism.md), with no `guards.md` (Ivan, 2026-10-26:
   ADR only). "What the sweeps do" in `ci-cd-determinism.md` and "What the guard does" in
   `assertion-determinism.md` are deleted.
5. Bind sections to checks: every check prints a short ID with its result, every rule section
   lists the IDs that hold it, and a test fails a section listing none, an ID no guard prints, and
   a printed ID no section lists, the way the acceptance tier binds product chapters to tests. The
   scope is the two CI/CD rules this split leaves, `ci-cd-determinism.md` and
   `workflow-inputs.md`, and the test scripts each names under "Enforced by" (Ivan, 2026-10-26);
   other rules keep today's form.
6. Faster guard: each workflow read once, and scripts no workflow names skipped (16.1 s to 6.8 s
   on a copy, identical results, 2026-10-26); then every sweep reads jobs, steps and env from one
   YAML read, in place of the `awk` slicing.
7. [`CLAUDE.md`](../../CLAUDE.md) indexes `workflow-inputs.md` with its guards, and the
   `ci-cd-determinism.md` row names what it keeps.
8. Other rules give their why and their checks' workings to existing ADRs, each keeping its
   instructions (Ivan, 2026-10-26, all four taken):
   - [`sonarcloud.md`](../rules/sonarcloud.md): why a green local scan is not a green gate, how
     CI reads the gate's answer, and why a red local scan can mislead, to ADR-012
     ([ADR-012](../../docs/adr/ADR-012-sonarcloud-gate.md)).
   - [`code-comments.md`](../rules/code-comments.md): how the checker reads and its modes, to
     [ADR-125](../../docs/adr/ADR-125-comments-and-repeated-code.md);
     [`docs-live-state.md`](../rules/docs-live-state.md): how the gate matches, to
     [ADR-080](../../docs/adr/ADR-080-documentation.md).
   - [`test-value.md`](../rules/test-value.md): the breakage-rate measurements, to
     [ADR-098](../../docs/adr/ADR-098-test-value.md);
     [`coverage-exclusions.md`](../rules/coverage-exclusions.md): the four lists and the report
     rewrite, to [ADR-091](../../docs/adr/ADR-091-coverage-reports.md);
     [`resource-conservation.md`](../rules/resource-conservation.md): slope against baseline and
     the scope, to [ADR-093](../../docs/adr/ADR-093-relay-session-lifetime.md).
   - [`refactor.md`](../rules/refactor.md): the copy of what the gauntlet runs becomes a link to
     the script; [`tool-versions.md`](../rules/tool-versions.md): how the dependency check reads
     declarations and image tags, to [ADR-126](../../docs/adr/ADR-126-advisory-exceptions-lapse.md).

## Also in this plan: five nightly workflows that keep going red

Ivan's request (2026-10-25): investigate five workflows and put the fix options here. Measured on
2026-10-25 from each run's logs, bundles and artifacts. Nothing below is edited until Ivan picks an
option per item; each pick lands in the commit [Commits](#commits-ivan-2026-10-25) names, test
first, with its ADR and docs edited in that commit.

One cause runs through items 1, 2 and 4. GitHub's hosted runners come from at least six processor
models (AMD EPYC 7763, 9V45 and 9V74; Intel Xeon 8370C, 8573C and 6973P-C, read from the Go
benchmark job's `cpu:` line over 29 nights), and one model is not steady either: the Go handshake
benchmark read 3 512, 3 727 and 10 311 ns on three EPYC 9V45 nights. A night compared with nights
on other machines measures the machine as much as the code.

### 1. E2E Performance — all 14 runs

2026-10-15 → 2026-10-25: 4 green, 9 red, 1 cancelled by hand (39913420184).

| What | Evidence |
|---|---|
| Run time doubled | The speed-spec step took 9–10 min until 10-22 and 23–24 min from 10-23, when six out-of-order cases joined (7 → 13 specs). The two 100 Mbit/s out-of-order cases take 9.5 of the 24 min. |
| Direct download collapses at 100 Mbit/s with 1% of packets out of order | Red 4 of 4 since added: 15.6–17.1 Mbit/s, 16–17% of the line against the 80% floor. The same case at 10 Mbit/s passes every run. |
| 5% out of order at 100 Mbit/s times out | Red 4 of 4: the 512 MiB file cannot finish inside the 5-minute test limit at that rate, although at 5% the share is only read, not held. |
| Typing echo at 100 Mbit/s over Direct with a download running | Red on 6 of 13 completed runs (10-15, 10-17, 10-20, 10-21, 10-23, 10-25): 95th-percentile echo 115–157 ms against 100 ms, with single stalls of 0.4–2 s; on 2 runs the download held 67% of the line. The same case through the server passes 13 of 13. |

Why:

- The machine's side of a Direct link is the `rtc-sctp` 0.21.1 crate (under `webrtc` 0.21). It
  declares a packet lost once three later packets are acknowledged (`miss_indicator >= 3` in
  `chunk_payload_data.rs`) and halves its sending window each time it enters recovery
  (`association/mod.rs`). It has no allowance for a packet that is only late, and never takes a cut
  back when the "lost" packet turns up. With 1% of packets displaced it keeps cutting, and the
  download settles near 16 Mbit/s whatever the line — above what a 10 Mbit/s line carries, which
  is why that case passes.
- One ordered channel carries keystrokes, screen and file pieces
  ([`session-channel.ts`](../../web/src/lib/transport/session-channel.ts) line 172), so every
  recovery holds the echo behind it. Through the server the same traffic rides the kernel's TCP,
  which tolerates reordering, and passes. That the echo stalls are the same recovery is likely, not
  proved: no run records the link's resends beside each echo.

RMM example: a technician downloads a 500 MB crash dump from a clinic's practice server over
Direct; the office router reorders 1% of packets under load, so the download runs at 16 Mbit/s on
a 100 Mbit/s line — four minutes for a forty-second job — and the shell they type into stutters.

Options:

1. **Teach the machine's sending side that a late packet is not a lost one** (recommended). When a
   resend proves needless (the original is acknowledged first), restore the window it cut, and
   raise the count of later packets needed before declaring a loss. Linux TCP does both: it learns
   how far a path reorders and undoes a cut made in error. QUIC's loss-recovery standard (RFC 9002)
   adds a time test beside the three-packet one. A change to `rtc-sctp`, offered upstream and used
   from a pinned revision until released — a dependency change that needs Ivan's approval
   ([`tool-versions.md`](../rules/tool-versions.md)). Test first: a reorder-only fake link in the
   crate's tests, then the spec.
2. **Move bulk downloads to the server path when Direct's measured rate falls below it**, keystrokes
   staying Direct. A product change to [ADR-127](../../docs/adr/ADR-127-direct-link.md); costs
   server bandwidth and leaves Direct itself slow.
3. **Give keystrokes a channel of their own** beside the file channel, with about one round trip of
   file data queued ahead of them. Chrome Remote Desktop keeps control and input on channels of
   their own. Alone it does not lift the 16 Mbit/s ceiling; take it only if the echo still stalls
   after option 1, proved by recording the link's resends beside each echo.
4. **The 5% case reads its rate over a fixed window, then stops the download**, so it measures in
   about a minute instead of timing out. Its mark stays "read".
5. **Run time:** options 1 and 4 together bring the step back near 13 min. Splitting the specs over
   two or three jobs, each with its own stack, cuts it further for a stack start (about 1.5 min) and
   a browser install per job; one stack cannot run them side by side, since a machine takes one
   session at a time.

The 80% floor at 1% out of order stays as set, whichever option is taken.

### 2. Benchmark Trends — the last 14 failures

| Nights | Cause | State |
|---|---|---|
| 7 (10-08 → 10-14) | `BenchmarkManager_SignAgent` and `SignServer` allocated 8.7% more (16 772 → 18 308 B/op) after the certificate split in 04369711 | A real rise, caught. Repaired by 4e9a10a5: 16 772 again from 10-15 |
| 1 (38667764379, 10-15, baseline update) | The summary after the update found a regression and ended the step before it reported | Repaired in 615f1d28: both calls read their own status |
| 6 (39399716849, 39892895541, 39930355657, 40079371545, 40137691068, 40179049585) | Rust `decode_server_hello` read 16.99–17.15 ns against a window median of 10.65 (line 15.98) | **Open** |
| 1 of those 6 (39892895541) | The re-measure job lost `BenchmarkHandshaker_PerformHandshake`: go test's `goos`/`pkg`/`cpu` lines reached benchstat | Repaired in 66e8bed2 |

Why `decode_server_hello` is red: one commit (c09372bf) read 8.36, 10.40, 10.71, 12.10, 16.98, 16.99
and 17.15 ns on different nights — a cluster near 17 ns and one at 8–12 ns, which is the machine,
not the code. The window median sat at 10.65 because 7 of its 14 nights were fast. Five slow
machines in a row since 10-23 keep it red, and it turns green by itself once slow nights are the
majority, again with no code change. A Rust timing is judged on one reading
([ADR-088](../../docs/adr/ADR-088-benchmarks-measure-the-code.md)) and the Rust job records no
processor model. The Go re-measure shares the weakness: it re-runs on another random machine and
compares with nights from mixed machines.

Options:

1. **Compare tonight's code with the last green night's code on the same machine** (recommended).
   The nightly builds both commits on one runner, runs them alternately, and fails only when the
   comparison (benchstat for Go, Criterion's own for Rust) finds a significant slowdown past the
   line. The machine cancels out. Roughly doubles the benchmark jobs' five minutes. Go's own
   performance dashboard compares this way, and it is the "relative" method continuous-benchmarking
   tools such as Bencher prescribe for shared runners. Rewrites the timing half of ADR-088 and
   [`benchmark-confirm.sh`](../../scripts/benchmark-confirm.sh); allocation counts stay as they
   are, since they do not depend on the machine.
2. **Record the processor model with every reading and compare only with nights on the same
   model**, reporting without a limit until three exist. Cheaper, but one model varied threefold
   (above), so it narrows the noise without removing it.
3. **Count instructions instead of time** for the Rust micro-benchmarks (a Valgrind-based harness,
   as CodSpeed uses). Identical on every machine, but a new dependency and harness (Ivan's
   approval), and blind to cache and memory-speed effects.

### 3. PMAT run 40177945275 (2026-10-25)

Files below B+ rose from 2 to 5. Graded locally with pmat 3.17.0 on `main` (b5253d09) and on
ed77bc11:

| File | Grade | Below since |
|---|---|---|
| `scripts/dependency-fixes/tools.py` | C+ (65.4) | 7eb60c84 |
| `scripts/dependency-fixes/versions.py` | B- (70.9) | 7eb60c84 |
| `scripts/dependency-fixes/images.py` | B- (74.0) | 7eb60c84 |
| `deploy/scripts/monitoring-readback.py` | C+ (66.4) | before ed77bc11 |
| `server/internal/api/openapi_gen.go` | C- (55.9) | generated |

Why the commit let them through: the commit-time grade check
([`pmat-precommit.sh`](../../scripts/pmat-precommit.sh)) grades only what
[`tdd-check.sh`](../../scripts/tdd-check.sh) `is-code` calls code — Go, Rust, TypeScript and
JavaScript. Python is never graded at commit; the nightly grades the whole tree. The two also count
different sets: the nightly counts generated code, the commit check skips it.

Options:

1. **Split the three files by concern until each clears, repair `monitoring-readback.py` with
   them, and have the commit check grade every language the nightly grades** (recommended). The
   next Python file below the floor is refused at commit. Test first: the commit check's shell test
   gains a Python file below the floor. The nightly skips generated files the way the commit check
   does, so both apply the same file filter: the commit check to the files a change touches, the
   nightly to every file.
2. Split the files only. Tonight goes green; the next Python file repeats this.
3. Leave it. The nightly compares with the night before, so it goes green tonight (5 → 5) with the
   files still below the floor: a regression is red for one night only. A fixed reference — no file
   below B+ but generated ones — keeps it red until repaired; item 5 has the same gap.

Also: the alert reads "regression on dev" though the nightly runs on `main`.

### 4. Performance Stack — the last six runs

| Run | Leg | What the run said |
|---|---|---|
| 39892972355 (10-23, by hand) | every leg | The technician scenarios lost `LOADTEST_PROFILE` when the generator moved under `sudo`. Repaired in 66e8bed2 (`--preserve-env`); 10-24's run by hand is green |
| 39952926055 (10-23) | scaling, 0.25 processors | 3.4% of the steady phase failed (ceiling 1%), so the run is thrown out as measuring nothing |
| 40091451495 (10-24) | scaling, 0.25 processors | 4.0%, the same |
| 40192556668 (10-25) | spike | 78% of offered arrivals arrived (floor 80%), thrown out as "the load was never offered" |
| 40042440858, 40137649441 | — | Green, both run by hand in the late evening |

What the bundles show:

- **The bottom rung got worse on 2026-10-22, on every night.** Machines cut off when the walk
  ended rose from 4–20 a night to 59–95. On slower nights enrolments now give up after the
  harness's 30-second wait: 59 and 67 failures, against 0–1 before at the same 15-second median
  enrolment. The server is 98% busy at that rung every night, before and after. Two changes landed
  that night and are not yet told apart: enrolment now writes a certificate record (615f1d28), and
  the harness and the technician scenarios now share one processor allowance instead of one each.
- **The generator's own room is no longer measured.** The allowance moved from the generator's own
  scope to a shared slice. The harness finds no limit on its own scope and falls back to a
  whole-machine run-queue reading, which reads 0% room on every 0.25 night since 10-23 (most likely
  the throttled server's waiting threads), and the per-phase room fields are empty.
  [ADR-101](../../docs/adr/ADR-101-load-profiles-and-limits.md) says a squeezed generator reports
  its own starvation; that check cannot fire now, so a run cannot tell a generator that fell behind
  from a server that could not keep up.
- **A failed leg keeps no record of its machine.** "Record what the runner and the server's
  allowance did" runs only when the harness step succeeds, so all three failed legs lack their
  processor model and throttling. The spike's arrival share ran 85% on EPYC 7763, 91% on 9V74 and
  99–99.7% on 9V45; the 78% night's machine is unknown.
- **The spike's floor reads the server as the generator.** The 80% floor compares arrivals that
  completed with arrivals offered. With the server 96% busy in the spike, a slow server lowers
  completions, and the verdict blames the generator and discards the night.

RMM example: Monday 08:00 a power cut ends and 2 000 machines at one customer reconnect at once. A
server that takes that wave at 78% is what the spike exists to report; today the run calls it
"not measured" and throws the number away.

Options, in order:

1. **Restore the evidence** (recommended, and the others depend on it): the runner record runs on
   every leg outcome, and the generator's room is read from the slice holding the allowance, which
   counts both generators. Test first: a fake cgroup tree with the limit on the parent.
2. **Tell the two 10-22 changes apart on one runner:** the 0.25 rung with c09372bf's server and with
   today's, same harness; then today's server with one shared generator allowance and with two
   separate ones. The numbers choose between 3a and 3b.
3. a. If enrolment got slower, make its new certificate record cheaper (written in one statement
   with the rest of enrolment, or after the answer). b. If the shared allowance starves the
   harness, give each generator its own allowance back, as ADR-101 states.
4. **Split the spike's floor in two:** a generator that fell behind its own schedule (counted on its
   own clock and its refused processor time) invalidates the run; a server that took arrivals
   slower than offered is a reading, judged by the profile's limits. k6 makes the same split:
   iterations it could not start on time are "dropped", apart from slow responses. Changes
   [`validity.go`](../../server/tests/loadtest/validity.go) and ADR-101.
5. Record the processor model in the trend rows so nights compare like with like (item 2,
   option 2).

Not offered: raising the bottom rung to 0.5 processors or lengthening the enrolment wait; both
change the experiment instead of the fault.

### 5. Mutation Testing — the last ten runs

Nine red, one cancelled by hand (39921426194, 10-23).

| Nights | Cause | State |
|---|---|---|
| 10-15 → 10-22 | The Go leg never completed. `TestAPIRequestsAreCountedUnderTheAccountThatMadeThem` (429 expected, 200 read) failed the coverage run gremlins makes across the whole module before mutating, so it failed whichever shards hit it | Repaired in 615f1d28 (a bucket that never refills); on 10-24 and 10-25 no shard hit it |
| 10-15 → 10-22 | The single web shard was cut off at its 90-minute cap on 6 nights | Repaired in 615f1d28 (three web shards) |
| 10-23 | `TestFastPathAcceptsAnyTrustedHash` flaked in go-api-incidents | Repaired in aadcea89 |
| 10-16, 10-24 | A runner was lost (rust-core-ml-backfill-drain; go-api-runtime after 59 min, "lost communication") | Once each; a failure on unchanged code waits until it recurs |
| **every night since 10-15** | **Web 83.1% against the 85% floor** (85.3% on 10-14): 197 more mutants survive or go untested, 150 of them in five files of the direct-link release — `session-channel.ts` 57, `direct/handoff.ts` 35, `SessionView.tsx` 27, `FileManagerView.tsx` 20, `download-saver.ts` 11 | **Open** |
| **10-24** | **Rust 91.9 → 88.7%**: 104 more missed mutants, mostly in the agent modules split out on 10-22 — `control_loop.rs` 34, `hardware_report.rs` 22, `lifecycle.rs` 16, `terminal/hangup.rs` 14, `terminal.rs` 8, `identity_store.rs` 6 | **Open.** Alerted once, then passed on 10-25 (88.7 → 88.7) because the drop rule compares with the night before |

Go stands at 86.3% (87.3% on 10-14) on its first complete night since 10-14, inside the 2-point
rule; untested Go mutants rose 386 → 472.

Options:

1. **Kill the survivors** (recommended): tests for the five web files and the six agent modules, each
   proved by a local mutation run on that file. Before writing agent tests, check that the mutation
   run executes the new tests that drive the agent as a separate process against the mutated build;
   if it does not, those modules read as untested whatever their tests do, and the fix belongs in
   the run. Some survivors may be equivalent mutants, as the register's Go entry found; each is
   judged, not assumed.
2. **Compare each leg with a fixed reference** — the best of the last 14 nights, or a committed
   floor per leg — so a drop stays red until repaired (item 3, option 3). Changes
   [`mutation-summarize.sh`](../../scripts/mutation-summarize.sh) and
   [ADR-122](../../docs/adr/ADR-122-mutation-walk-and-legs.md).
3. Only if the runner loss recurs: re-split the shards over 60 minutes. go-api-identity took 75 of
   its 90 minutes on 10-25 (56 on 10-21), go-api-limits 73 on 10-24.

### Ivan's picks (2026-10-25)

- **Item 1:** options 1 (late-packet tolerance; approves a pinned `rtc-sctp` revision until the fix
  is released upstream), 3 (a keystroke channel of its own) and 4 (the 5% case reads over a fixed
  window). Added by Ivan: the workflow records a runner summary like the Performance Stack's —
  processor model, the share of time the host took back, and how often the server's container was
  throttled — in the run summary, on every outcome.
- **Item 2:** option 1 against an anchor commit. Each night builds the anchor and tonight's commit
  on one runner, runs whole suites in turns, 10 rounds a side for Go and for Rust, so every
  benchmark is judged (Ivan, 2026-10-25). Measured: one Go suite run takes about 43 s and the Rust
  suite measures for about 2.5 min, so the Rust job runs about 50 min, its 30-minute cap rises to
  about 70, and the night finishes about 35 min later; the rounds run only in the nightly, and the
  local gauntlet is unchanged. `baseline.json` names every benchmark the suites run, so the
  missing-benchmark rule covers each judged one (today it names 19 of 20 Go and 5 of 17 Rust). It
  fails only on a slowdown benchstat (or the same test over Criterion's estimates) finds
  significant and past the limit. The anchor moves only by a commit accepting a change, the role
  `history_from` plays today; the same commit on both sides is recorded, not judged. A benchmark `benchmarks/baseline.json` names that
  is missing from tonight's run fails the night (today's check judges only what it read, so a
  deleted or renamed benchmark leaves judgement silently); removing or renaming one edits
  `baseline.json` in the same commit, and the gauntlet's local benchmark check refuses the same,
  so a rename is caught at commit (Ivan, after the outside review). For Rust, the gauntlet's
  existing test-mode run already prints every benchmark name (`Testing decode_server_hello`), and
  those names are compared with `baseline.json`'s Rust rows both ways; for Go,
  [`benchmark-harness.test.sh`](../../scripts/tests/benchmark-harness.test.sh) already refuses a
  row naming no benchmark (Ivan, 2026-10-26, after the second outside review). A benchmark absent at the
  anchor is reported until the anchor moves. Allocation checks and the trend rows stay; the
  re-measure job's work moves into this comparison. Rewrites the timing half of ADR-088. Added by
  Ivan: the workflow records a runner summary like the Performance Stack's, per job (the Go and
  Rust jobs run on different machines). Evidence it rests on (2026-10-25): on one machine the red
  `decode_server_hello` read 7.94 ns at c09372bf and 7.99 ns at b5253d09 (+0.6%, nightly said
  +60%); the handshake read no difference against itself (p=0.74) and −4.5% old against new
  (p=0.002). The per-model alternative, simulated over 29 Go nights, left 248 of 497 readings
  unjudged and still raised one alarm on unchanged code.
- **Item 3:** option 1 (split the three files, repair `monitoring-readback.py`, the commit check
  grades every language the nightly grades, the nightly skips generated files; measured
  2026-10-25, the nightly's check reads Go, Rust, TypeScript/JavaScript and Python, and no YAML or
  shell, so Python is the language the commit check gains). pmat's grade check holds a Python file
  to B whatever floor it is asked for ("required B" at B+ and at A+), so both checks hold Python
  to B+ by each file's own score, 80 or more (Ivan, 2026-10-25), and `scripts/dependency-fixes/charts.py`
  (79.2, the one Python file between B and B+ of 35) is split with the others. Also taken: the reference
  chosen for item 5: no file below B+ but generated code, and a committed repo-score floor, beside
  today's night-before check. The trend store and dashboard are unchanged. The commit check reads
  pmat's version from the manifest instead of its own default (`3.17.0` in `pmat-precommit.sh`,
  which no sweep holds to the manifest), so an approved pmat move reaches both checks (Ivan, after
  the outside review).
- **Item 4:** options 1 (restore the evidence), 2 (tell the two 10-22 changes apart on one runner,
  then 3a or 3b by the numbers) and 5 (processor model in the trend rows). Option 4, staged (Ivan,
  after the outside review): commit 2 records the generator's own lateness (arrivals it started
  behind its schedule, and its refused processor time) beside the arrival share, watched and not
  enforced, as ADR-101 requires of a new reading; commit 3 makes a server that took arrivals
  slower than offered a reading judged by the spike's limits, and a generator that fell behind its
  own schedule invalidates the run in place of the 80% floor. Today the spike reached 85% of
  offered arrivals on EPYC 7763, five points above the floor, so server-limited nights would keep
  going red as "not measured". The 78% was technician arrivals (`AchievedFraction` reads them
  first when a phase offers them), and the spike profile gates only the machines' side (error
  rate, connect time), so the split needs a technician judge (Ivan, 2026-10-25): commit 2 records
  the share of technician arrivals the server completed and their 95th-percentile time in the
  spike, watched; commit 3 gates whichever the watched nights bracket, with values from those
  nights.
- **Item 5:** options 1 (tests for the survivors, after checking that the mutation run drives the
  agent's separate-process tests against the mutated build; the survivors in `session-channel.ts`
  and `direct/handoff.ts` are counted again and tested after item 1's per-channel rewrite, which
  replaces that code in the same commit) and a runner summary per shard, like
  the Performance Stack's. Option 2 as a committed floor per language, set slightly under the
  current score so timeouts do not flap it, beside today's 2-point night-before check; the nightly
  offers a raised floor when scores rise, as the benchmark baseline candidate does. The stored
  trend and the mutation-trend dashboard are unchanged (they read the same rows whatever the
  gate compares with).

### Ivan's answers to the open points (2026-10-25)

- **Item 1, a channel per job:** three channels — typing and the terminal's replies, the screen,
  the files — and the patched `rtc-sctp` sends from them by weighted fair queueing as RFC 8260
  section 3.6 defines it, by bytes rather than by message, instead of from its one first-come
  queue: it sends next from the channel whose bytes sent divided by its weight is lowest, a whole
  message at a time, with equal weights a setting can change (the RFC 8831 section 6.4 weights).
  RFC 8260 section 3 requires this scheduler of a WebRTC stack. Keystrokes still go first, being
  small. Chrome's SCTP takes turns per message while interleaving is off, its default, and shares
  by bytes only with interleaving on (dcSCTP `stream_scheduler.h`) (Ivan, 2026-10-26, after the
  second outside review). Every message is at most 16 KiB (`MAX_PIECE`, the link's maximum
  message size), so without message interleaving a reply waits behind at most one piece per busy
  channel on the machine's side (Ivan, after the outside review). Interleaving (RFC 8260 I-DATA)
  stays out: RFC 8831 section 6.6 makes it a SHOULD and names a 16 KB message limit as the
  alternative, and Chrome negotiates it only under the `WebRTC-DataChannelMessageInterleaving`
  field trial (`dcsctp_transport.cc`). Test first: two channels kept busy, one with 16 KiB and one
  with 4 KiB messages, share the bytes sent within one message; a keystroke queued behind both
  waits at most one piece per busy channel. MeshCentral keeps a tunnel per job (terminal, desktop, files),
  RustDesk a connection per job, Chrome Remote Desktop separate input and control channels, RDP
  prioritised virtual channels. Built regardless of whether the echo still stalls after the
  out-of-order fix. Option 5 (splitting the speed specs over jobs) is declined.
- **Item 1, one hand-off per channel** (Ivan, after the outside review): the hand-off between relay
  and link assumes one ordered stream per direction. The sender numbers every frame in one
  sequence (`handoff.rs` `send`), the receiver counts frames in arrival order (`deliver_link`),
  `DirectAck` and `DirectFallback` carry one `received` count that frees that many frames from
  the front of the sender's list, one reassembler joins 16 KiB pieces into 256 KiB file frames,
  and one 4 MiB window bounds what is unconfirmed (`can_take`). Over three channels unchanged,
  pieces of different frames interleave and the join fails, a reply counted ahead of an earlier
  file frame frees that frame so a fall-back never re-sends it, and a full window holds the reply
  back. So each channel gets its own numbering, confirmations, 4 MiB window and reassembler; the
  confirmation and fall-back notices carry one count per channel. Changed together in the agent
  (`handoff.rs`, the writer in `peer.rs`), the browser (`web/src/lib/direct/handoff.ts`) and the
  message types in `server/internal/protocol/control.go` with their golden files. Test first: a
  fall-back in the middle of a download with typing interleaved, every frame delivered once and in
  order on its own channel.
- **Item 1, the channels as one unit** (Ivan, 2026-10-25): everything the agent's interactive lane
  carries today (keystrokes, terminal output, control messages, file listings, desktop input)
  rides the typing channel; screen frames ride the screen channel; file data in both directions
  rides the files channel. The session moves to the link when all three are open and falls back
  to the relay together when any one fails, the notices carrying one count per channel; the three
  share one connection, so a real failure takes all three anyway.
- **Item 1, pacing and windows** (Ivan, 2026-10-25): one send limit for the path, measured from
  every channel's confirmations and applied to screen and file bytes; typing is exempt, since its
  lane caps it at 256 KiB. Replay windows: typing 256 KiB, screen 4 MiB, files 4 MiB, so a side
  holds at most 8.25 MiB per session (4 MiB today). This is the split SCTP (RFC 9260 section 7.2,
  congestion control per association), QUIC (RFC 9000 section 4, RFC 9002 section 7) and Chrome's
  own SCTP stack make: how fast to send is decided once per path, how much is held is bounded per
  stream. At 100 Mbit/s and a 30 ms round trip about 430 KiB waits at the agent with screen and
  files busy, and the screen takes the whole path at once when a download ends.
- **Item 1, one writer for screen and files** (Ivan, 2026-10-26, after the second outside review):
  today's writer holds one channel's own unconfirmed bytes under the limit (`write` in
  [`peer.rs`](../../agent/crates/mesh-agent-core/src/link/peer.rs)), and that count is per channel
  down to `rtc-sctp` (released per stream on acknowledgement), so a writer per channel would let
  screen and files each fill the whole limit: about 860 KiB at 100 Mbit/s, and the 128 KiB floor
  twice over on a slow line. One task feeds both channels: it sends next from the one with fewer
  bytes sent, and only while the two channels' unconfirmed bytes together, plus the piece, fit the
  limit. Typing keeps its own writer. RDP and Citrix schedule their channels from one sender the
  same way. A connection per job (MeshCentral, RustDesk) was weighed and declined: two allowances
  queue about twice the data at the customer's router (derived from `pacing.rs`: about 170 ms
  against 65 ms at 10 Mbit/s and 40 ms), the screen's own window grows only while fully used (RFC
  9260 section 7.2.1) so a screen burst starts small, the session takes two or three shares of the
  customer's line, and setup and router mappings triple (RFC 8843 section 1). Test first, on a
  fake clock: both channels kept busy, their unconfirmed bytes together never exceed the limit
  plus one 16 KiB piece, and each gets its byte share within one piece. No live test between a
  real agent and a browser: no agent captures a screen today
  ([`control_loop.rs`](../../agent/crates/mesh-agent/src/control_loop.rs) hands every session
  `NullCapture`, and no agent lists `RemoteDesktop`), so commit 2 writes a Low register entry for
  it, paid down by a Windows agent with screen capture (Ivan, 2026-10-26).
- **Item 1, uploads** (Ivan, 2026-10-26): in an upload Chrome's SCTP sends, and it resends on the
  third report and halves its window with no time rule (dcSCTP `outstanding_data.cc`,
  `retransmission_queue.cc`), so the patched agent leaves uploads on a reordering line as slow as
  today. The speed spec gains an upload over Direct at 100 Mbit/s with 1% out of order, the whole
  512 MiB file to completion as the download case does, under a 6-minute limit of its own (Ivan,
  2026-10-26; at about 16 Mbit/s it takes about 4.5 minutes), marked "read" with no floor, and
  [`Remote-Sessions.md`](../../docs/product/Remote-Sessions.md) states the behaviour. MeshCentral
  sends files through its server by default and RustDesk over the operating system's TCP; Chrome
  Remote Desktop shares the weakness.
- **Item 1, acceptance** (Ivan, 2026-10-26): the feature's high-level acceptance tests are the
  three browser specs already run on every commit with a real agent, kept unchanged so a pass
  means what it means today: [`direct-link.spec.ts`](../../web/e2e/direct-link.spec.ts) "reaches
  Direct, and the relay carries no session data once it has" and "a 1 s drop keeps the session
  Direct; a 5 s drop moves it to the relay once, keystrokes intact", and
  [`downloads.spec.ts`](../../web/e2e/downloads.spec.ts) "the link lost during a download moves the
  session to the relay with nothing lost" (typing during the drop, every keystroke in order, the
  file's checksum whole), which is the cross-channel fall-back the per-channel hand-off exists
  for. No new acceptance test, and no test of an older agent against the new release at this
  point. This follows the critical-path-only practice of Chrome Remote Desktop's connection tests
  and Pion's browser tests; MeshCentral and RustDesk carry no such suite.
- **Item 1, slow lines** (Ivan, 2026-10-26): the agent's limit never falls below 128 KiB
  (`FLOOR` in [`pacing.rs`](../../agent/crates/mesh-agent-core/src/link/pacing.rs)), which is
  about 0.5 s of data waiting at a 2 Mbit/s router during a download, and typing waits behind it
  there whatever the agent's scheduler does (derived, not measured; nothing runs below 10 Mbit/s).
  Chrome's SCTP lets its window fall to 4 packets. The speed spec gains typing echo with a
  download at 2 Mbit/s and 40 ms on both paths, marked "read", in commit 2; the numbers go to Ivan
  before the floor or the 64 KiB cushion changes.
- **Item 1, older agents:** the agent states whether it accepts the separate channels; when it does
  not, the browser keeps everything on the one shared channel, as today (the way Chrome Remote
  Desktop lists capabilities on both sides and kept a fallback until every host had updated). The
  statement is a new `AgentCapability` entry, which reaches the browser on the device record the
  way `RemoteDesktop` does (`SessionView.tsx` reads `capabilities`); an agent that lists it still
  accepts the one shared channel from a browser tab opened before the release.
- **Item 1, late packets** (Ivan, 2026-10-26, after the second outside review; this replaces the
  count-based wording of option 1): RFC 8985 as written, adapted to SCTP as in Weinrank's work.
  1. A piece is lost once a piece sent more than the reordering allowance after it is
     acknowledged. The allowance starts at a quarter of the least round trip, grows by that much in
     each round trip whose acknowledgements list a duplicate, is capped at the smoothed round trip,
     and stays grown for 16 recoveries (RFC 8985 section 6.2). SCTP's duplicate list stands in for
     TCP's DSACK: Chrome's receiver fills it (dcSCTP `data_tracker.cc`), and `rtc-sctp` 0.21.1
     writes it (`association/mod.rs`) but its sender never reads it.
  2. The three-report rule starts recovery only while no reordering has been seen (RFC 8985
     section 6.2). Pion keeps it unconditionally (`association.go`, reported in pion/sctp#389 on
     2026-08-24 and still in `main`), so Pion's design is not copied on this point.
  3. Undo: when the duplicate list names a piece that was resent, the resend was needless, and the
     window and threshold return to their values before the cut. RFC 8985 leaves the response to
     other specifications (section 9.3); this follows Linux's undo and the Eifel response (RFC
     4015).
  4. A probe at the end of each burst, with the resend timer re-armed after it (RFC 8985 section
     7.3; pion/sctp#498 fixes a missing re-arm).
  5. The receiving side acknowledges at once when a gap appears (Pion measured about 30% in its
     reordering tests from this alone); `rtc-sctp` 0.21.1 already does (`has_packet_loss` in
     `association/mod.rs`), so nothing changes here.

  Pion's published numbers (+44% in an unordered test mixing 1.5% loss with reordering at an
  unstated rate, +35% with no loss) do not cover an ordered, reorder-only path, so nothing here
  rests on them. The test rig sets a trap: [`router.sh`](../../deploy/router/router.sh) sends a
  reordered packet without its 20 ms delay, so round-trip samples on it read about 20 ms and the
  starting allowance (5 ms) is a quarter of the displacement; only the growing allowance covers
  it. Throughput alone misleads: the agent's limit holds unconfirmed bytes near rate × round trip
  + 64 KiB, 1.13 times the line at 100 Mbit/s and 40 ms, so each needless cut halves the rate
  there, while at 10 Mbit/s the 128 KiB floor is 2.6 times the line and hides it. Tests first, in
  the crate, asserting on the window: reordering only (no cut left standing once the allowance
  has grown, every earlier cut undone); real loss (cuts made and kept); tail loss (recovered by
  the probe before the resend timer); a round trip moving 40 → 20 → 40 ms; delayed
  acknowledgements; reordering and loss together. Then the speed spec.
- **Item 2, the comparison:** a slowdown fails when it is significant and past 10% (today's +50%
  was sized for readings from different machines). One anchor commit for every benchmark, moved
  only by a commit accepting a change. The first anchor is the last `main` commit before commit 2,
  so commit 2 is judged on its first night: it rewrites the code under `direct_link_terminal_echo`
  and `direct_link_file_piece` (`direct_bench.rs`, the hand-off) and `send_limit_interval`
  (`pacing_bench.rs`) (Ivan, 2026-10-26, after the second outside review). Commit 2 keeps those
  names and measures the same work under them, since a renamed benchmark is absent at the anchor
  and goes unjudged. Commit 2 against itself runs once by hand as a noise check, not as the anchor.
- **Item 1, the patched `rtc-sctp`:** a copy under Ivan's GitHub account (`volchanskyi/rtc`), used
  through Cargo's `[patch.crates-io]` and pinned by commit, removed once released. In the same
  change: `agent/deny.toml` allows that one git source, and the dependency check, whose crate
  reader takes only `registry+` sources today, reads the pinned copy too. The two changes go to
  the maintainers (`webrtc-rs/rtc`) as two pull requests to `master`, opened from that copy, each
  behind its own setting, off by default, which the agent switches on (Ivan, 2026-10-25):
  `feat(sctp)` time-based loss detection (RFC 8985, with the undo of a needless cut) and
  `feat(sctp)` weighted fair queueing
  (RFC 8260 section 3.6), each with its own tests. The repository asks only for a pull request;
  its merged pull requests carry one concern each, under titles of that shape, and `master` (at
  0.21.1) is copied into the 0.21 branch in bulk before each 0.21 release, so a merged change
  reaches the agent as a 0.21 patch. The scheduler touches the send queue that #253 and #259
  rewrote and open #279 changes, so it stands apart from the loss fix. Ivan sees each text before
  it is sent.
- **Item 4:** after the one-runner measurement, the numbers go to Ivan before 3a or 3b is touched,
  whichever change they point at.
- **Item 5, floors:** each language's bar is the higher of 85% and its committed floor; the floor
  sits 0.5 point under its score (on unchanged code a score moved at most 0.2 points a night over
  24 runs) and is first set from the night after that language's survivor tests land.
- **Item 3:** the repo-score floor is exactly 64.0 (the score read 64.0 on 20 nights over six
  commits). The alert names the branch it graded, `main`, in the same change. If Ivan approves the
  pmat move, the floor follows the new version's reading of the same tree, set in that commit.

## Also in this plan: the Go mutants the survey skips (low-hanging techdebt)

Ivan's pick (2026-10-25): step 1 of the register's Medium entry "Most of the Go mutation gap is not
a test gap".

### What is wrong

- gremlins (`TOOL_VERSION_GREMLINS`, 0.6.0) runs a mutant only when its position falls inside a
  block of the coverage profile (`IsCovered` in its `internal/coverage/profile.go`). Go's profile
  opens a block at a `case` clause's body, never at its expression, so every mutant on a `case`
  expression reads NOT COVERED and is skipped unrun: 84 of them on 2026-09-23.
- Upstream issue go-gremlins/gremlins#298 (opened 2026-07-16, reproduced by two more users) is
  open. No release since v0.6.0 (2025-12-06); the open pull requests are unmerged.

### The fix

1. Test first: in the copy's own tests, the #298 reproduction — a tagless switch whose every
   clause runs — reports its `case` mutants covered; `case a, b:` entered through `a` leaves `b`
   unread; a switch holding `fallthrough` stays unread; here, the sweep holding the manifest to
   the workflows names the pinned copy.
2. A copy under Ivan's GitHub account (`volchanskyi/gremlins`), pinned by commit, in which a
   position on a `case` expression counts as covered by this rule (Ivan, after the outside
   review): a clause's own body proves only its first expression, since `case a, b:` runs its body
   once `a` matches without evaluating `b`; the body of a later clause or of the `default` proves
   every expression above it; a switch holding `fallthrough` keeps today's reading, since a body
   entered through it proves nothing about its own expressions. A switch that matches nothing and
   has no `default` stays unread, as today: gremlins' coverage run reads whether each block ran,
   not how often. On `server/` (2026-10-25) 108 mutants sit on case expressions, 8 of them after
   the first expression of their clause, and no switch holds `fallthrough`. The copy publishes
   under its own module path, `github.com/volchanskyi/gremlins`, in one rename commit on top of
   the fix (Ivan, after the outside review): `go install cmd@version` refuses a module carrying
   `replace`, so a copy under the upstream path cannot be installed by version, and the
   dependency check's commit reader reads only a Rust lockfile. It installs the way benchstat
   does: `TOOL_VERSION_GREMLINS` holds its commit-based version and `TOOL_SOURCE_GREMLINS` its
   path, and all three install lines — the two in `mutation.yml` and the one in `require-tool.sh`
   — name that path. The version check (`go version -m`), the version sweep and the dependency
   check's Go reader read it unchanged, and Go's checksum log records the code.
3. Offered to the maintainers as a pull request I open from a branch of that copy holding only the
   fix, without the rename; Ivan sees the text first.
4. The register's Go entry is edited to what remains (step 2, the survivors), with the numbers of
   the night after commit 2.

## Commits (Ivan, 2026-10-25)

As few as the waits allow, whatever their size.

1. **Commit 1:** the staging-drill fix, with `!cancelled()` on both deploys and the drills, and
   the CI/CD rule split (Ivan, 2026-10-26), and the other rules' moves to ADRs (step 8).
   Built 2026-10-27, uncommitted; left: the gauntlet, `/refactor`, the commit, CI, CD.
   Measured on the built tree: `ci-cd-determinism.test.sh` 14.7 s to 0.6 s and
   `workflow-inputs.test.sh` 1.0 s, with every count of the old guard reproduced (61 calls and
   122 inputs identical, compared row by row); the status sweep named 9 jobs and 18 jobs
   outliving a cancel, each fixed; `fault-ci.test.sh` evaluates the drill and deploy conditions
   over nine outcomes and failed four on the old `cd.yml`. Alert sends report a publish job a
   cancel skipped as "cancelled before publishing". One new ADR, 129, for verdicts and workflow
   inputs both (Ivan, 2026-10-27: ADR-130 merged into it).
   Also in commit 1 (Ivan, 2026-10-27): the combined log of ADR-001 … ADR-012 is split into one
   file per ADR under `docs/adr/`, the log deleted, every link repointed, and the index check
   requires each row to link its own file; `cargo-nextest` moves 0.9.149 → 0.9.150, the patch
   the dependency check required.
   Also in commit 1 (Ivan, 2026-10-27): `qs` 6.16.0 → 6.16.1 in `web/package-lock.json`
   (GHSA-6pq8-jhm2-2vcw), set by hand since `npm update` took 6.17.0; the check's npm advice
   names the lockfile entry and the fixed release, never `npm update`; and the hand gauntlet
   run before `/refactor` is gone — the order is `/refactor`, then the commit's one gauntlet run,
   with the pass marker, the `/refactor` start hook and every mention of the hand run removed.
2. **Commit 2, everything that waits on nothing:**
   - item 1: the patched `rtc-sctp` (time-based loss detection with undo, and sharing by bytes
     between channels) with `agent/deny.toml` and the dependency check's reader, the three
     channels with one hand-off per channel and one writer for screen and files, their capability
     and fallback, the 5% case's fixed window, the upload reading, the 2 Mbit/s echo reading, the
     runner summary, and the three Direct-link browser specs passing unchanged;
   - item 2: the anchor comparison at the 10% limit over whole suites, 10 rounds a side, the first
     anchor the last `main` commit before commit 2, benchmark names kept across the rewrite, the
     Rust job's cap raised to about 70 min, every suite benchmark named in `baseline.json`, a
     missing named benchmark failing at night and at commit (Rust names read from the gauntlet's
     test-mode run), and its runner summaries;
   - item 3: the split files (the three, and `charts.py`), Python graded at commit and at night by
     score against B+, generated files skipped at night, the
     no-file-below-B+ reference, the 64.0 repo-score floor, the alert naming `main`, the commit
     check's pmat version read from the manifest;
   - item 4: options 1 and 5, the generator's lateness and the two technician readings recorded
     beside the arrival share (watched), and the inputs the one-runner measurement needs;
   - item 5: the survivor tests (after checking the run drives the agent's separate-process tests),
     the per-shard runner summary, and the floor mechanism with the 85% minimum, floors unset;
   - the Go switch-mutant fix above;
   - a Low register entry beside "The Chat tab is unreachable from any machine the browser stack
     can run" (Ivan asked for it, 2026-10-26): "Screen and file traffic are never tested together
     on a real direct link". It states that one writer feeds both channels under one limit and a
     fake-clock test holds them to it, that no real agent and browser run the two together because
     every session gets the null screen source, and that a fault in how the real link releases or
     adds up the two channels' bytes would reach production unseen. **Pay-down trigger:** a
     Windows agent with screen capture; the speed spec then keeps the screen busy during a
     download, reading the router's queued bytes against one allowance and each channel's share;
   - every dependency move approved by then.
3. **Commit 3, after a night of commit 2:**
   - the committed mutation floors, 0.5 point under each language's score that night;
   - item 4's fix, as Ivan chooses from the numbers the one-runner measurement brings back, and
     the spike's verdict split (a server's shortfall judged by the technician gate the watched
     nights bracket, a lagging generator invalid);
   - the remaining approved dependency moves;
   - a Low register entry for the patched copies of `rtc-sctp` and gremlins, triggered by each
     maintainer release that carries the fix (Ivan approved the entry, 2026-10-25);
   - this plan's deletion and its `phases.md` row.

Between commits 2 and 3: the 2 Mbit/s echo numbers go to Ivan, the one-runner measurement is dispatched, the three pull requests (two to
`webrtc-rs/rtc`, one to `go-gremlins/gremlins`) go to the maintainers once Ivan has read them, and the dependency rows are brought to Ivan one at a time, the
rows customers' traffic runs through first.
