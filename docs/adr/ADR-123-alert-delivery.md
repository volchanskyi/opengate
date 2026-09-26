---
number: 123
title: An alert channel is proven by a delivered message
---

# ADR-123 — An alert channel is proven by a delivered message

## Context

Two alert channels were dead at the same time, for different reasons, and
neither said so.

Grafana held exactly one destination — a placeholder email address — and routed
every rule to it, including a critical one that was firing during the check. The
Telegram destination the repository described was created by hand in the user
interface. What that interface writes lives in Grafana's database, which sits on
a volume that does not outlive the pod, so the only copy of the destination died
with a reschedule that had happened before anybody was looking.

The nightlies were silent for a different reason. Every alert step was
conditioned on a flag an earlier step in the same job produced, so a night that
died before that step reported nothing at all. One hundred and forty-eight runs
across six workflows produced four sends in three weeks, and three further
nightlies carried no alert step. Every one of those steps also ended its failure
paths with `exit 0`, so a step that tried to deliver and could not was green.

Underneath both sat a third thing. The cluster's alert rules, dashboards and
scrape targets are ConfigMaps created once by hand and re-created by nothing: the
repository declared thirteen alert rules and the cluster evaluated seven, and
among the six missing was the one written to detect that per-container metrics
were not being collected — which they were not, because the scrape job it reads
had never been applied either.

The repository's own note said file provisioning of a Telegram destination was
impossible on this Grafana. Re-tested, that is nearly right and the conclusion
drawn from it was too broad: what fails is environment-variable substitution into
the numeric chat-id field, which the server reads back as a JSON number and
refuses to start on. A literal string in the file works and reads back as
file-provisioned.

## Decision

**One send, and it fails when the message did not arrive.**
[`telegram-alert.sh`](../../scripts/telegram-alert.sh) is the only path to the
chat. It refuses a revoked token, a chat the bot is not in, a 200 carrying
`ok:false`, an absent credential, and a request that reached nothing — because a
send that swallows its own status reports a refusal as success.

**The send runs under `always()` and decides for itself.** It reads the job's own
status beside whatever flag it was watching, and the message names which of the
three happened: a finding, a night that failed, or a run somebody cancelled.
Where a workflow has several jobs, or one job whose timeout is barely over what
the work takes, the alert is a job with `needs:` and `if: always()` rather than a
step — a job killed at its timeout may run no step at all.

**A refused send is allowed to fail because the verdict lives elsewhere.** Every
one of these workflows carries its regression verdict in a separate gate job
reading `needs.<job>.result`; `pmat-trend` and `terraform-drift` gained one here.
So a Telegram outage reddens the job that sent and hides nothing.

**The destination is provisioned from a file, not through the interface or the
API.** The chat id is substituted into the file as a quoted literal by the
applier, since Grafana cannot substitute it itself. A file is re-read on every
pod start, which is the property that makes the destination survive a
reschedule.

**The cluster's monitoring configuration is rendered, applied and read back.**
[`monitoring-config-apply.sh`](../../deploy/scripts/monitoring-config-apply.sh)
renders the three ConfigMaps from the canonical files, applies what differs,
asks for it back, and refuses on a difference. It restarts only what changed,
each reader once, by the kind the chart runs it as — the store is a StatefulSet,
and its applier's test reads the kinds from the chart rather than trusting a
name. The nightly infrastructure-drift workflow runs it, so the cluster is
compared with the repository every night rather than at an install nobody
repeated. The chart those ConfigMaps belong beside follows the same way: the
production deploy upgrades the monitoring release from it every time, so a
permission or argument the chart gains is not left waiting on a hand install.

**A message carries what the reader needs to act, and nothing Grafana keeps for
itself.** Grafana is reachable only through a port-forward, so the message is
the whole of what the person reading it has. Every rule states, in its own
units, the reading and the line it crossed (`observed`) and the first thing to
look at (`check`);
[`message-templates.yml`](../../deploy/grafana/provisioning/alerting/message-templates.yml)
prints those with the series' own labels and the time it began, says "no data"
in words when that is the finding, and leaves out Grafana's bookkeeping labels.
The text is sent unparsed, because summaries carry characters Telegram refuses
as HTML.

**And a real message goes through the channel every night.** All of the above can
be correct and still reach nobody. That job cannot alert through the channel it
is testing, so the workflow's own result is the signal that survives.

## Consequences

A file-provisioned destination is read-only in Grafana's user interface. That is
the point — nobody can silently break it — and it also means an operator can no
longer route or silence by hand. Chosen, rather than discovered later.

A malformed provisioning file stops Grafana starting, taking the dashboards with
it. Loud rather than silent is the right direction, but it means the file needs a
gate before the ConfigMap is updated;
[`alert-rules.test.sh`](../../scripts/tests/alert-rules.test.sh) is the precedent
and reads the file rather than the cluster.

The chat id is not a credential: possession of it grants nothing without the bot
token, which stays in the Secret and substitutes correctly because a token is not
a number.

An absent Telegram secret now fails a nightly rather than warning. A rotated
secret is a dead channel, and a dead channel that reports success is the whole
subject of this decision.

The applier's read-back cannot see one failure: a chat id whose value carries its
own quotes is stored verbatim by Grafana and rejected by Telegram at send time, a
green apply with a broken destination. The applier refuses that value by name
before applying it.

`cd.yml` has no alert path and is deliberately outside the sweep: a deploy is
triggered by somebody already watching it. Whether that holds is a separate
question.

The standing rule is [`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md).
