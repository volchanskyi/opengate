# Staging E2E runs against real enrolled machines

## The fault

`Run Playwright E2E against staging` fails 14 specs, every one of them timing
out at 30s inside a `beforeEach` that calls `enrolledMachine(...)`. That helper
polls `/api/v1/devices` for the hostnames `agent-a` and `agent-b` to report
`online`. Those two machines are containers
[`docker-compose.test.yml`](../../../deploy/docker-compose.test.yml) starts for the
local stack. Staging runs none, so the specs cannot pass there.

[`playwright.staging.config.ts`](../../../web/playwright.staging.config.ts) states
the assumption that broke: *"the same specs … a local pass predicts a staging
pass"*, deleting only `webServer`. Deleting `webServer` also removed the two
machines, and nothing noticed.
[`playwright-config-parity.test.sh`](../../../scripts/tests/playwright-config-parity.test.sh)
holds the two configs together and forbids the staging side from redeclaring
`projects` or `testDir`, so the suite is the same suite by construction.

Specs affected: `device-site-dnd` (×2), `file-manager` (×3), `hardware`,
`inventory`, `restart` (×3), `session-terminal` (×4).

Introduced by `99ae5090` (2026-08-20), which moved these specs from mocked
routes onto real enrolled machines. First staging failure: the next CD run,
2026-08-21. Staging has deployed nothing since, and production has been promoted
nothing since.

This is the only thing still failing in that job. `3229bad5` repaired the chart
hook that was failing the release before the tests ran at all, and the 2026-08-26
19:32 run reached Playwright and failed on nothing but these 14 specs.

## The decision

Give staging the two machines, rather than narrowing the suite that runs there.
The flows involved — opening a terminal, listing files, reading hardware
inventory, restarting an agent — are the ones most likely to break on real
infrastructure rather than in compose, and they are exactly what a deploy gate
is for.

[`agent.Dockerfile`](../../../deploy/agent.Dockerfile) already records that a
containerized agent is a supported shape and that Terminal and File Manager are
the capabilities the browser specs exercise against it, so no agent change is
needed.

## The constraints that shape the work

**The staging node is arm64** — a single Always-Free A1.Flex worker, 2 OCPU and
12 GB, carrying production and staging both
([`main.tf`](../../../deploy/terraform/main.tf)). Whatever runs there is small, and
its binary is built for that chip.

**The machines cannot run on the CD runner.** An agent speaks QUIC over UDP and
`kubectl port-forward` carries TCP only. They run **inside the namespace**,
which is the conclusion [`load-test.yml`](../../../.github/workflows/load-test.yml)
reached for the same reason — it runs its fleet from a pod in
`opengate-staging`.

**The staging certificate names `localhost` and nothing else.**
`server.quicHost` is empty in
[`values-staging.yaml`](../../../deploy/helm/opengate/values-staging.yaml), and
[`agentapi/server.go`](../../../server/internal/agentapi/server.go) adds a name to
the certificate only when it is set. The agent derives its TLS name from the
host half of `OPENGATE_SERVER_ADDR`
([`main.rs`](../../../agent/crates/mesh-agent/src/main.rs)), and has no way to be
told a different one. So whatever name the machines dial has to be on the
certificate.

**The server's Service carries TCP 8080 only**
([`server-service.yaml`](../../../deploy/helm/opengate/templates/server-service.yaml)).
There is no UDP port to reach the QUIC listener through. The load run works
around this by addressing the server pod's own address, and that path has
carried a hundred QUIC agents nightly.

## How the machines get there

The deploy job cross-builds the agent for the node's chip and copies the binary
into a stock Alpine pod — what the load run already does with its Go harness.

The alternative considered and rejected was publishing an `opengate-agent`
image beside the server image. The premise that pushed toward it — that no arm64
build exists — is not so:
[`release-agent.yml`](../../../.github/workflows/release-agent.yml) cross-builds
`aarch64-unknown-linux-musl` on every agent release, and the last six runs of it
took between two and a half and six minutes. Against that, an image needs its
own change-gate (`agent/**` is explicitly *not* an input to the server image's
gate, per [`build-image-gate.sh`](../../../scripts/build-image-gate.sh)), its own
tag-forward job, its own signing and scanning, and a way for the cluster to pull
a package that is private on creation. And because its gate would be separate
from the server's, a deploy could pair one commit's server with another commit's
machines. Building in the deploy job makes that impossible rather than guarded.

## The ordering trap

Three things want to own the first row of the `users` table, and only one can.

[`handlers_auth.go`](../../../server/internal/api/handlers_auth.go) promotes a
registrant to administrator only when it is the **only** row in `users`.
`global-setup.ts` relies on that: it registers `bootstrap-admin@test.local` and
then throws if the account is not an administrator.

So the reset has to leave the table empty, and the first thing to register after
it has to be the bootstrap operator. The machines' enrolment token is minted by
that same operator, before Playwright starts — `global-setup.ts` then finds the
account already there, falls back to its login path, and gets the same
administrator token.

This is why the load-test administrator is **not** exempted from the truncation.
Sparing it would make the bootstrap operator the second row, leave it unpromoted,
and fail every spec in the suite before one of them ran.

## The load-test administrator

`Reset staging DB for E2E` truncates `users` and `security_group_members`,
destroying the account the post-upgrade hook seeded four steps earlier in the
same job. The nightly load run has had no administrator to mint against on every
night following a deploy, and has been red since 2026-08-22.

It is put back **after** the browser suite finishes, where it disturbs nothing.
The statements that seed it move to one file the chart hook and the deploy job
both read, so the pair cannot drift — the shape
[`pg-app-role-sql.sh`](../../../deploy/scripts/pg-app-role-sql.sh) already uses for
the app-role password, and for the same reason: the password reaches psql on
standard input, never on a command line a pod's process list or the API server's
exec audit record would carry.

## Steps

### 1. Put the in-cluster name on the certificate

[`server-deployment.yaml`](../../../deploy/helm/opengate/templates/server-deployment.yaml)
sets `OPENGATE_QUIC_HOST` only when `server.quicHost` is configured. Give it a
default of the release's own server Service name, which is the name a pod in the
namespace reaches the server by in every deployment. Production keeps its public
name; staging gains a certificate the in-cluster machines can verify.

### 2. Share the load-test account's SQL

- `deploy/helm/opengate/files/loadtest-account.sql` — the statements, with the
  email and the password taken from psql variables the caller sets.
- The chart hook reads it with `.Files.Get` instead of restating it.
- `deploy/scripts/loadtest-account-sql.sh` — emits the two `\set` lines then the
  file, for piping into `psql` over standard input.

### 3. Build the agent in the deploy job

In [`cd.yml`](../../../.github/workflows/cd.yml)'s `deploy-staging-k8s`, after the
kubeconfig is available and before the Helm upgrade:

- Read the architecture of the node the staging server runs on, the way
  [`load-test.yml`](../../../.github/workflows/load-test.yml) does, and map it to a
  musl target.
- Build `mesh-agent` for that target — `cross` for arm64, `cargo` for amd64 —
  with the same pinned actions and cache
  [`release-agent.yml`](../../../.github/workflows/release-agent.yml) uses.

### 4. Bring the two machines up

After `Reset staging DB for E2E`, because the reset truncates `devices`:

- Register the bootstrap operator through the public endpoint and mint an
  enrolment token as that account — the same two calls
  [`e2e-stack-up.sh`](../../../deploy/scripts/e2e-stack-up.sh) makes for the local
  stack. The token goes into a namespace Secret; it is masked in the log and is
  in no checkout.
- Read the server pod's address.
- Create two pods **named `agent-a` and `agent-b`**. A pod's hostname is its
  name, which is what
  [`enrolled-machine.ts`](../../../web/e2e/helpers/enrolled-machine.ts) matches on.
  Each pod carries a host entry mapping the server Service's name to the server
  pod's address, so the name the machine dials is the name on the certificate
  and the packets take the path the load run has proven. A node selector on the
  architecture keeps a wrong-chip binary Pending rather than crash-looping.
- Wait for both to report `online` through the API before the suite starts.

### 5. Take them away, and put the administrator back

In `if: always()` steps beside the existing `Stop port-forward`:

- Delete both pods and the enrolment Secret.
- Re-seed the load-test administrator from the emitter in step 2.

The device rows need no removal — the next run's reset truncates `devices`, and
[`global-teardown.ts`](../../../web/e2e/global-teardown.ts) polices sites, not
machines.

### 6. Make the failure legible

[`enrolled-machine.ts`](../../../web/e2e/helpers/enrolled-machine.ts) gives itself
a 30s deadline and [`playwright.config.ts`](../../../web/playwright.config.ts) sets
the per-test timeout to the same 30s, so the test dies before the helper can
throw. Its message names the fleet it actually saw — *"the machine agent-a never
came online. The fleet holds: an empty fleet"* — and that message has never once
been printed. Drop the deadline below the test timeout, and have the message
name both stacks rather than only the local one.

### 7. The tests that would have caught it

Each of these fails against the tree as it stands:

- Every hostname `enrolled-machine.ts` pins is brought up by **both** the local
  stack and the staging deploy. A spec requiring a machine neither provides
  fails the gate. (`e2e-stack-machines.test.sh`, which already owns the local
  half.)
- The name the staging machines dial is the name the chart puts on the
  certificate. (`e2e-stack-machines.test.sh`.)
- The helper's deadline is strictly less than the config's per-test timeout.
  (`e2e-stack-machines.test.sh`.)
- The machines and their credential are removed whatever the suite's verdict,
  and the load-test administrator is put back. (`cd-workflow.test.sh`.)
- The account's password never rides a command line, and the chart and the
  deploy job seed it from the same file. (`loadtest-account-sql.test.sh`.)
- The shared SQL is held against the schema the migrations build, like the SQL
  already embedded in the chart and the workflows.
  (`deploy_sql_schema_test.go`.)

## Files

| File | Change |
|---|---|
| [`deploy/helm/opengate/templates/server-deployment.yaml`](../../../deploy/helm/opengate/templates/server-deployment.yaml) | in-cluster server name as the default certificate name |
| `deploy/helm/opengate/files/loadtest-account.sql` | the seeding statements, one copy |
| [`deploy/helm/opengate/templates/loadtest-service-account-job.yaml`](../../../deploy/helm/opengate/templates/loadtest-service-account-job.yaml) | read the file instead of restating it |
| `deploy/scripts/loadtest-account-sql.sh` | emit the same file for piping into psql |
| [`.github/workflows/cd.yml`](../../../.github/workflows/cd.yml) | build the agent, mint, two pods, wait, tear down, re-seed |
| [`web/e2e/helpers/enrolled-machine.ts`](../../../web/e2e/helpers/enrolled-machine.ts) | deadline below the test timeout; message names both stacks |
| [`scripts/tests/e2e-stack-machines.test.sh`](../../../scripts/tests/e2e-stack-machines.test.sh) | the staging half of the same three invariants |
| [`scripts/tests/cd-workflow.test.sh`](../../../scripts/tests/cd-workflow.test.sh) | teardown and re-seed |
| `scripts/tests/loadtest-account-sql.test.sh` | the emitter |
| [`server/internal/db/deploy_sql_schema_test.go`](../../../server/internal/db/deploy_sql_schema_test.go) | scan the chart's files directory too |
| [`docs/infrastructure/Testing.md`](../../../docs/infrastructure/Testing.md) | what the staging run stands up |
| ADR + [`.claude/decisions.md`](../../decisions.md) | staging E2E runs against real machines |

## Reviewer checklist

- [ ] No certificate authority key leaves the cluster; the pods enrol through
      the public endpoint with a minted token.
- [ ] The enrolment token is minted per run and expires; it is in no checkout
      and is masked in the log.
- [ ] Pods are named `agent-a`/`agent-b` and their hostnames reach the API as
      those names.
- [ ] The bootstrap operator is the first account registered after the reset, so
      it is promoted and `global-setup.ts` finds an administrator.
- [ ] Teardown removes both pods and the credential even when the suite fails.
- [ ] The load-test administrator is present after a CD run; the nightly load
      run mints against it.
- [ ] A second CD run in a row is green — the first proves bring-up, the second
      proves teardown left nothing behind.
- [ ] `playwright-config-parity.test.sh` still passes; the staging config gains
      no `projects`/`testDir` override.
- [ ] The new gates fail against the current tree.
