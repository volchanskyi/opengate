---
number: 128
title: A machine's identity is kept across a reinstall, renewed before it expires, admitted expired only to renew or be removed, and signed by a master key only the server can read
---

# ADR-128 — A machine's identity is kept across a reinstall, renewed before it expires, admitted expired only to renew or be removed, and signed by a master key only the server can read

## Context

A virtual machine on the maintainer's workstation could not be added back after
its agent was reinstalled. Its device had been deleted while the machine was off,
and the reinstall kept the identity files on disk: the agent enrolled only when
an identity file was missing, so it ignored the new join code and connected as
the deleted device. The server recognised the deleted number every time and
wrote the removal order in the same instant it closed the connection, which
discarded the order unsent. The agent read every attempt as a lost connection
and retried every 20 seconds, indefinitely. The fast reconnect path refused a
stale master-key fingerprint the same way, so the agent's fall-back to a full
handshake never fired.

Looking at the whole of a machine's identity found more. Machine certificates
lasted 365 days and nothing renewed them. The master key could not be replaced
without reinstalling every machine. A join code could obtain a certificate for
an existing machine's number, because the server signed whatever subject the
request named. Two machines holding one identity both stayed connected and wrote
under one device. And the master key sat in the same Kubernetes secret as the
database passwords, readable by every workflow: they signed in to the cloud as a
tenancy administrator, which on OKE holds every Kubernetes right.

## Decision

**The removal order always arrives.** A deleted machine that connects is sent
`AgentDeregistered`, the server closes its own sending side, and only once the
machine hangs up — or after
[`removalOrderWait`](../../server/internal/agentapi/admission.go) — does it close
with code 3. The agent treats code 3 with no frame exactly as the frame, and
closes its sending side as soon as it reads the frame so the server's wait ends
at once. The close codes are named once on each side
([`close_codes.go`](../../server/internal/protocol/close_codes.go),
[`close_code.rs`](../../agent/crates/mesh-protocol/src/close_code.rs)) and pinned
to one fixture, [`close_codes.json`](../../testdata/golden/close_codes.json):

| Code | Meaning | The agent |
|---|---|---|
| 0 | Normal close | Reconnects |
| 1 | The control stream could not be accepted | Reconnects |
| 2 | Handshake refused; on the fast path, a stale master-key fingerprint | Does a full handshake next time |
| 3 | Device deleted | Applies the removal order |
| 4 | Restricted session ended: renewed, or its time ran out | Reconnects at once after a renewal |
| 5 | Certificate refused: too old, superseded, unvouched, or never issued here | Reconnects with backoff, logging the reason hourly |
| 6 | Replaced by a newer connection for the same device | Reconnects with backoff, logging hourly that another connection took its identity |

An update's post-install check clears on the server's acknowledgement — the
first `TrustBundle` or `CertificateIssued` of a connection, which the server
sends only after its deny-list check — and never on the agent's own send, so an
update whose first connection is refused stays unverified and the rollback
watchdog keeps running.

**Self-removal finishes after any interruption.** In
[`lifecycle.rs`](../../agent/crates/mesh-agent/src/lifecycle.rs) the agent writes
a `removed` marker and flushes it, wipes its data folder and trusted master keys,
disables and deletes its unit, deletes its binary, removes the marker and the
folders, and exits 0. It never stops its own service: the unit restarts only on
failure, so nothing restarts it. The wipe comes before the disable because a
killed process is restarted while its unit is loaded, but after a power cut only
an enabled unit starts; disabling first could leave the identity and up to
512 MiB of readings on a machine that never starts again. A marker found at
start-up with no install note finishes the remaining steps without contacting
the server. The most a power cut between the last steps leaves is the binary and
an empty data folder holding the marker, which the next install clears.

**A reinstall recovers on its own, decided from one table.** The installer
([`install.sh`](../../server/internal/api/install.sh)) leaves an install note —
join code, server address, and the master keys the server announced — at
`0600`, written to a temporary file and renamed into place, and restarts the
agent so a re-run runs the new binary. The note is the only way a code reaches
the agent: the unit carries no code and the agent takes no flag for one, so a
deleted machine never holds a code it could re-enrol itself with. The agent
decides at start-up and again on a removal order, and a note beats both a
`removed` marker and a removal order:

| On the machine | The agent |
|---|---|
| `removed` marker, note present | Deletes the marker; the rows below apply |
| `removed` marker, no note | Finishes self-removal |
| No identity, note present | Enrols with the note |
| Note marked `fresh` (`install.sh --fresh`, for a cloned template) | Wipes and enrols |
| Identity issued by none of the note's master keys | A different server: wipes and enrols |
| Identity the server acknowledges | Keeps it and deletes the note unused |
| Identity the server says is deleted, note present | Wipes, enrols with the note, and restarts into the new identity |
| Identity the server says is deleted, no note | Removes itself |
| Certificate refused, note present | Wipes and enrols |
| Certificate refused, or the server's chain trusted by none of its master keys, no note | Retries with backoff and logs the reason hourly |
| Note's code refused | Keeps the note, logs the server's reason, exits non-zero |

The wipe on "deleted" is what keeps the catch-up upload from re-sending an
erased device's history under its new identity. A different server is detected
by overlap: no master key in common between the note and the machine, and the
machine's certificate issued by none of the note's keys.

**The note is removed in the same all-or-nothing save as the new identity.** The
agent's identity files are written through a crash-safe store
([`identity_store.rs`](../../agent/crates/mesh-agent-core/src/identity_store.rs)):
staged copies, then a commit marker listing each file and its hash, then the
renames. Recovery runs before anything reads the identity: no marker rolls
back, a marker rolls forward whatever matches its hash, and the live pair is
never deleted. Enrolment and renewal both save through it, and the note's
removal and the master keys it carried are listed in the same commit. After a
crash anywhere a machine holds its old state and the note, or its new identity
and no note, so a `fresh` note can never enrol twice and orphan a device.

**The server chooses the machine number.** Enrolment ignores the subject of the
request, generates a number with no device, deleted or certificate row
([`issuer.go`](../../server/internal/devicecert/issuer.go)), issues the
certificate with it and returns it beside the certificate. The agent stores the
number its certificate carries, and refuses a reply whose number differs from
its certificate's. A join code therefore cannot claim an existing machine, by
construction. Intune, Tailscale and AWS IoT fleet provisioning assign the
number on the service side the same way.

**Certificates last 90 days and renew over the live connection from day 60.** A
leaf is backdated by a clock-skew margin, never outlives its issuing master key,
and is not issued past that key's expiry
([`cert_issue.go`](../../server/internal/cert/cert_issue.go)). The server, on its
own clock, offers renewal at registration and on a daily sweep once two thirds
of the presented certificate's own life has passed, or when its issuer is not
the current one ([`renewal.go`](../../server/internal/agentapi/renewal.go),
[`renewal_sweep.go`](../../server/internal/agentapi/renewal_sweep.go)). It makes
no offer when the issuing master key's own expiry would cap the new certificate
at the old one's end, since that only issues ever-shorter certificates; the
daily census warns instead. The agent makes a fresh P-256 key in memory, sends a
request naming its own number, and checks the reply before saving anything: the
key matches, every block of the trust bundle parses, the bundle verifies the
server chain of the live connection, and a full TLS configuration builds
([`cert_renewal.rs`](../../agent/crates/mesh-agent/src/cert_renewal.rs)). It then
saves, swaps its TLS configuration with a fresh ticket cache, and sends
`CertificateInstalled`. A device gets at most three issuances in a rolling day,
counted from the ledger so the count survives a restart; a lost reply uses one
of them, so it can delay a renewal by a day inside a 30-day margin but never
block it. cert-manager's default life and Puppet's renewed-certificate default
are 90 days too. The server's own QUIC and Intel AMT gateway certificates are
served from a cache and re-signed once within
[`serverRefreshWindow`](../../server/internal/cert/cert_sign_server.go) of their
expiry, so a long-running process never serves an expired one.

**Installed means current, and current supersedes.** Every issued certificate is
recorded in a ledger before it is sent
([`store.go`](../../server/internal/certledger/store.go), migration
[`020`](../../server/internal/db/migrations/020_device_certificates.up.sql)), and
nothing is sent if the record fails. A certificate becomes the device's current
one when the machine reports it installed on the connection it was issued on, or
when it is first presented if that report was lost, and it supersedes every
earlier one at once: any live connection still on a superseded certificate is
closed with code 5. A stolen old key stops working the moment the real machine
renews. A daily prune deletes rows that are superseded and past their expiry
plus the grace, which can admit nothing.

**Strict mode: a certificate the server never issued is refused.** A certificate
whose fingerprint is not in the ledger is refused with code 5 even when it
chains to a trusted master key, so a leaked master key mints nothing usable.
Admission reads an in-memory index warmed from the ledger at start-up and
updated on every issuance, and writes the first sighting behind the admission,
so a slow database never delays a sign-in and admission never fails open. The
index costs 333 bytes per certificate and the deny list 126 bytes per deleted
machine, measured over 100 000 entries by
[`tombstone_cost_test.go`](../../server/internal/agentapi/tombstone_cost_test.go).

**An expired certificate is admitted only to renew or be removed.** TLS asks for
any client certificate, and one pure function,
[`Classify`](../../server/internal/cert/classify.go), runs in `VerifyConnection`
— which Go runs on resumed handshakes too, where `VerifyPeerCertificate` is
skipped — and again in the application to learn the connection's standing. It
verifies the chain against every trusted master key at the certificate's issue
time, ignoring intermediates the client supplies, and fails TLS for a missing,
malformed, not-yet-valid or foreign certificate. Otherwise it returns a
standing: full; renew-only, for an expired certificate or a retired master key;
needs-voucher, for a master key marked compromised; or too old, past the grace
the `-cert-expired-grace` flag sets. Too old is refused with code 5, not by TLS,
so a deleted machine of any age still receives its removal order. A renew-only
connection is served by
[`conn_restricted.go`](../../server/internal/agentapi/conn_restricted.go): a
connection holding no repository, never listed as online, that accepts only
registration, a renewal request and the installed notice, and closes with code
4 after renewing or after `restrictedSessionLimit`. A ticket for an expired
certificate never resumes, and early data stays off.

**The master key is replaceable without reinstalling a machine.** Its role comes
from which file exists ([`trust.go`](../../server/internal/cert/trust.go)): the
issuer, a key accepted in full, one admitted renew-only, one admitted only with a
voucher from before a compromise date, the chain sent after the server's leaf,
and the key that signs the Intel AMT gateway's certificate. The server refuses
to start on a key that does not match its certificate, a certificate in two
roles, a chain entry that does not verify, a vouched-only key with no
compromise date, a key file group or others can read, and — under
`-ca-must-exist`, which production sets — a missing issuer, so it never
generates a new master key over a lost one. A change of key runs in phases:

| Phase | Issues | Also admitted | Machines are sent |
|---|---|---|---|
| steady | A | – | A |
| changeover | B | A in full | B and A |
| retire | B | A renew-only | B |
| emergency | B | A only with a voucher | B |
| settle | B | A renew-only until its last certificate lapses, then nothing | B |

The server presents its leaf from B with B's key cross-signed by A
([`cross.go`](../../server/internal/cert/cross.go)), so a machine that still
trusts only A verifies through it — even after A's own expiry, since rustls
ignores a root's dates — and the fast path accepts a cached fingerprint equal to
any trusted master key, so a changeover forces no fleet-wide full handshakes.
During a changeover the sweep runs every 10 minutes and offers an
`authority-change` renewal to every machine on A. In an emergency a machine
whose certificate was issued before the compromise date renews to B within one
reconnect, and any other is refused and needs a reinstall. Signing the Intel AMT
gateway's certificate with B breaks AMT until each device is reprovisioned with
B's root, so that key moves last and on its own. The routine changeover for the
current master key's 2036 expiry starts by early 2035.

**Two connections for one identity: the newest wins, and a live older one is
shown.** A second connection for a device replaces the first, which is closed
with code 6. Before closing it the server pings it and waits up to
`livenessProbeWait`
([`server_connection.go`](../../server/internal/agentapi/server_connection.go));
the newcomer proceeds meanwhile. A pong means two live machines hold one
identity: the server logs it once an hour per device, and records the time and
both addresses on the device row (migration
[`021`](../../server/internal/db/migrations/021_shared_identity.up.sql)). For a
day the device page says the identity is in use on two machines and to reinstall
one with `--fresh`. No pong means the older connection was already dead, and
nothing is recorded. MQTT, AWS IoT and MeshCentral keep the newer connection the
same way. The first copy to install a renewal keeps the device; every other copy
is refused as superseded.

**Only the server can read the master key.** Its role files live in a secret of
their own, `opengate-authority`, mounted only into the server pod's start-up
step, which copies them into a memory volume readable by the server's user alone
(the `authority-stage` step in
[`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml)).
Every step that writes a master key — the first, each phase of a change, and the
one-time move out of the secret it shared with the database passwords — runs the
[`opengate-authority`](../../server/cmd/opengate-authority/main.go) tool as a
job inside the cluster ([`authority-job.sh`](../../scripts/authority-job.sh)),
which prints roles and fingerprints and never a key, so no copy reaches an
operator's machine. Teleport, AWS IoT and step-ca keep such a key where it
cannot be copied at all; narrowing who can read it and never copying it out of
the cluster is the half this deployment can hold.

- **The cluster refuses any other reader.** A cluster-wide admission rule,
  [`authority-readers.yaml`](../../deploy/cluster/authority-readers.yaml), refuses
  any pod in `opengate` that mounts the secret or reads it into an environment
  variable unless it runs as the server's account with every container on the
  server's image. It is cluster-scoped, so no namespaced login can edit it, and
  each production deploy reads back that it exists and refuses the shapes it
  names ([`authority-readers-check.sh`](../../deploy/scripts/authority-readers-check.sh)).
- **The workflows sign in as a login that cannot read it.** A cloud user and
  group of their own ([`workflow-identity`](../../deploy/terraform/modules/workflow-identity/))
  may only fetch a kubeconfig, and everything they may do inside the cluster is
  named in [`workflow-access.yaml`](../../deploy/cluster/workflow-access.yaml):
  no `list` or `watch` on any secret, and nothing naming `opengate-authority`.
  Helm keeps its release records in configmaps (`HELM_DRIVER=configmap`), since
  its default needs `list` on secrets, which returns every secret's contents;
  [`helm-release-migrate.sh`](../../deploy/scripts/helm-release-migrate.sh) copies
  the existing records across once. Each deploy asks, as its own login, whether
  it may read the key's secret or list secrets, and fails on a yes
  ([`workflow-access-readback.sh`](../../deploy/scripts/workflow-access-readback.sh)).
- **What the narrowed login cannot write moves to a job a person approves.**
  Namespaces, the admission rule, the workflow access, the certificate issuer,
  monitoring's cluster roles and the ingress controller are applied by the
  `cluster` job in [`cd.yml`](../../.github/workflows/cd.yml)
  ([`cluster-apply.sh`](../../deploy/scripts/cluster-apply.sh)) under the
  `production` environment, holding the administrator login as an environment
  secret. It runs only when a commit changes one of those pieces or the running
  commit cannot be read, and the nightly drift check keeps reading them.

## Consequences

- No machine installed before this decision can connect after it: its
  certificate is not in the ledger. Every agent is reinstalled once after the
  deploy, and each appears as a new device.
- A machine switched off for more than about 120 to 180 days, depending on when
  it last renewed, is refused and needs a reinstall, which makes it a new device.
  A deleted machine receives its removal order at any age.
- Expired admission lengthens a stolen key's life: a key taken from a machine
  that is switched off and never deleted can be renewed up to the grace past its
  expiry. It dies when the real machine renews, when the device is deleted, or
  when the grace passes.
- A clone or a restored snapshot works only until one copy renews; the others
  are refused as superseded and need a `--fresh` reinstall.
- Client certificates are now judged by this codebase, so a defect in
  `Classify` is an authentication bypass. It is one pure function at both call
  sites, the cryptography is still `x509.Verify`, and a negative matrix and a
  fuzz target hold it.
- A deleted machine away for longer than its master key cannot be told to
  remove itself once that key is dropped; its own copy of the data stays until
  someone uninstalls it by hand, and Data Erasure says so. Intune and Teleport
  treat a machine that missed a key change the same way.
- The cross certificate relies on rustls ignoring a root's expiry. If a later
  rustls checks it, only machines still trusting nothing but the old key after
  its expiry are affected, and the census lists them.
- The certificate index, the deny list and the connection list each assume one
  server copy, as production runs. A second copy would not see a certificate the
  other issued, and strict mode would refuse it there; this is the first
  constraint a scale-out lifts.
- Whoever can change the server's deployment can still read the key: the
  admission rule checks the account and the image, and the server's image
  carries a shell, so a deployment running that image with another command — or
  a doctored image shipped through the deploy path — passes it.
- The workflows' login can still reach the key through the node's filesystem.
  One node runs production and staging, no namespace limits what a pod may
  mount, and that login may start pods in `opengate-staging` and `monitoring`
  for load tests, drills and metric pushes, so such a pod can mount the node's
  filesystem, under which the server pod's volumes sit. The network drill also
  runs commands inside monitoring's node exporter, which already mounts the
  node's whole filesystem. The fix is Kubernetes'
  `baseline` pod-safety level on both OpenGate namespaces, with the nightlies
  reaching monitoring through the cluster's pass-through to the metrics store and
  Grafana.
- The workflows' login keeps the namespaced rights a release and the nightlies
  use: it writes the Services, Ingresses and network policies each Helm release
  creates, so a stolen workflow key could point an address at a pod of its own,
  and it runs commands inside the production database pod and in staging and
  monitoring. The infrastructure scan accepts both for that one manifest alone
  ([`.trivyignore.yaml`](../../.trivyignore.yaml)).
- The ingress controller reads every secret in the cluster, the key's included,
  because it finds each site's certificate that way and Kubernetes offers it no
  narrower right. It is the most exposed holder of that right: in 2025 a flaw
  let anyone who reached it run commands with its permissions (CVE-2025-1974).
  The fix is a namespace of its own for the edge, holding the sites and their
  certificates, with the controller reading secrets there alone.
