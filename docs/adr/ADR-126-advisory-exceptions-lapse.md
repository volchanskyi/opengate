---
number: 126
title: Every dependency is checked for a hole a patch release fixes, and an exception lapses on its own
---

# ADR-126 — Every dependency is checked for a hole a patch release fixes, and an exception lapses on its own

## Context

The lockfile audits read the advisory database as it stands on the day, not the
diff, so an advisory published today fails every commit from then on. Usually
the answer is a version bump.

Those audits read three kinds of dependency: npm packages, Go modules and Rust
crates. Everything else the project runs went unread — the pinned tools and
what each is built from, the GitHub Actions, the container images, the charts
installed into the cluster, the Terraform providers and plugins, and the
Kubernetes version. Measured on 23 October 2026, Helm, Trivy and oapi-codegen
each carried advisories with a fixed release, and every image carried packages
with one.

On 2 October 2026 GitHub reviewed GHSA-vfj7-8cjw-p6xm against `braces`: every
release is affected, none is patched, and the maintainer disputes the report.
`braces` reaches the web tree only through `micromatch`, inside three
development tools already on their newest releases, which read only patterns
written in this repository. With no release to move to, the audit refused every
commit, and nothing in the repository could change its answer.

## Decision

- [`scripts/dependency-fixes`](../../scripts/dependency-fixes/) reads every
  dependency the repository declares and refuses the commit while an advisory of
  any severity has a fix in a patch release of the line in use, as
  [`tool-versions.md`](../../.claude/rules/tool-versions.md) defines one. It is
  the gauntlet's first check after the prerequisites, and a step of CI's
  security-audit job.
- Each finding gets one of three answers. A patch fixes it, and the commit is
  refused with the move that takes the fix. Only a newer minor or major fixes
  it, and it is listed for Ivan. No release fixes it, and it passes.
- A release is judged by what it is built from as well as by itself: a tool, a
  chart's program or a Terraform provider carrying an affected module counts as
  patch-fixed when its own newest patch release builds past it. An image counts
  as patch-fixed when the newest patch tag of the same shape, built for every
  platform the tag in use is built for, no longer carries the finding; a tag
  that names a line, such as `17-alpine`, is read at the build it points at. A
  tag that drops a platform is never the fix: the cluster's node is arm64, and
  a move to a tag pushed for amd64 alone stops the pod at its first start.
- A declaration no reader reads fails the check, and so does a lookup that gives
  no answer it can read. The production deploy reads back every image the
  cluster runs and fails on one the repository does not declare
  ([`running-images-check.sh`](../../deploy/scripts/running-images-check.sh)).
- An image's package list is read once per digest and cached, so a run judges
  it against the day's database without downloading its layers. CI keeps the
  cache and reads its save back.
- [`npm-audit.sh`](../../scripts/npm-audit.sh) is the only npm audit. The
  gauntlet and CI's security-audit job run it once per lockfile directory. It
  fails on any high or critical advisory that
  [`npm-audit-exceptions.json`](../../scripts/lib/npm-audit-exceptions.json)
  does not name for that directory.
- An exception names one advisory, the directory, the package, the newest
  release of that package when the entry was written, a review date and a
  reason. Every field is required.
- An exception fails the audit on its own when its review date passes, when its
  package publishes any other release, or when the audit stops reporting its
  advisory. A new release may be the fix; an advisory that is withdrawn or
  outgrown leaves an entry that excuses nothing.
- An exception is for an advisory no patch release fixes: one with no fixed
  release, or one fixed only in a newer minor or major that Ivan has not taken.
  An advisory a patch fixes is fixed, and no exception gets it past the
  dependency check.
- An audit or a lookup that cannot be read is retried after a pause; one that
  reports a finding runs once.

## Consequences

- A commit is refused from the day a patch for any dependency's advisory is
  published until the patch is taken.
- An advisory only a newer minor or major fixes never blocks a commit; moving
  to that line waits for Ivan's approval.
- The first read of an image digest costs a download of its layers, minutes
  for the largest; every later run takes seconds per image.
- The check asks outside services — OSV, the registries, GitHub. One that does
  not answer fails the commit rather than passing it.
- An npm advisory with no patch fix blocks until someone writes down why it is
  tolerable and until when, in a change reviewed like any other.
- Every other advisory in the same directory still blocks, including a second
  one against the same package.
- On the review date the gauntlet and CI fail until the entry is renewed with a
  fresh reason or removed.
