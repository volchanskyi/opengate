---
number: 121
title: Signed over-the-air agent updates
---

# ADR-121 — Signed over-the-air agent updates

## Context

Agents run on machines with no SSH and no other way in. A new version has to
reach them over the connection they already hold, and a bad update must not
leave a machine unreachable.

## Decision

**The server signs, the agent verifies.** An Ed25519 keypair is generated on
first run and persisted to `data/update-signing.json`. The server signs the
SHA-256 hash of each binary; the agent verifies with the public key it received
at enrollment, or one given on the command line.

**One manifest per operating system and processor**, at
`data/manifests/{os}-{arch}.json`, carrying the version, the download address,
the hash and the signature. Manifests can be published by hand or synced from
GitHub Releases.

**The server pushes; agents do not poll.** An administrator publishes a manifest
and pushes it, and the server sends `AgentUpdate` to eligible connected agents.

**The agent compares versions and refuses to go backwards.** An incoming version
at or below the running one is acknowledged as already current and skipped.
Otherwise it downloads, checks the hash, checks the signature over that hash,
then replaces itself.

**A failed update rolls back.** A watchdog restores the previous binary if the
new one does not come up. On SELinux hosts the replaced binary is relabelled, or
it cannot execute.

## Consequences

Losing the signing key means no agent accepts an update until the new public key
reaches it through enrollment.
