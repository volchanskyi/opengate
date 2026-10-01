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

Once messages arrived, most of them were about the tests. One scrape job reads
the production and the staging server and kept nothing naming which, so every
rule over the server summed the two: seventeen alerts the staging server raised
during a network drill read as production's rule pack running at five times its
ceiling. The rules watching the node fired at every nightly test that drove it
on purpose, and a container memory rule filtered its quotient rather than its
divisor, so six containers with no memory limit read as +Inf in every message
all night.

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
name. The production deploy runs it right after it upgrades the monitoring
release, so dashboards, rules and scrape targets land with the deploy, and the
nightly infrastructure-drift workflow runs it again, so the cluster is compared
with the repository every night rather than at an install nobody repeated. The
chart those ConfigMaps belong beside follows the same way: the production deploy
upgrades the monitoring release from it every time, so a permission or argument
the chart gains is not left waiting on a hand install.

**What a process loaded is asked of the process.** A ConfigMap is what a process
was given: a scrape relabel sat in its ConfigMap for two days while the running
store scraped without it, because the upgrade that wrote it restarted nothing
and the nightly apply then found the ConfigMap current — and every production
rule read nothing. So the applier asks the store for the configuration it is
running, reloads it until that is the declared file, and refuses if it never
is; the decision rests on what the process loaded, never on what the script
changed. Each pod that reads its configuration at start (the store, Loki and
Promtail) carries a checksum of it, so a chart upgrade that changes the
configuration restarts what reads it.

**Every panel answers, and every production rule can see.** After the reload
the applier runs every dashboard query against the store, each live one once per
environment
([`monitoring-readback.py`](../../deploy/scripts/monitoring-readback.py)). A
query that returns nothing, on a panel that does not say in words what empty
means, is a refusal naming the dashboard and the panel. A panel for a signal the
product does not produce is removed rather than left empty, and a counter with a
fixed set of outcomes is published at zero from start-up, so a panel or rule
over it reads a series before its first event. Every selector a
`watches: production` rule reads is then asked for a series, so a rule that
cannot see anything is found by the deploy rather than by the incident it
missed. After a deploy both checks wait for new targets' first readings before
judging.

**Every live reading names its environment.** Production and staging feed one
store and one set of dashboards, so each live dashboard carries an Environment
selector, Production by default, and every query names it — or the monitoring
namespace, for the monitoring stack's own readings
([`grafana-live-dashboards.test.sh`](../../scripts/tests/grafana-live-dashboards.test.sh)).
Each database is measured by an exporter in its own namespace, so its readings
carry its environment, and the server stamps what it writes to the store with the
namespace it runs in. Every read the server makes groups that stamp away: a
device lives in one environment, and a reading written before the stamp and one
written after it are one line on its chart.

**A message carries what the reader needs to act, and nothing Grafana keeps for
itself.** Grafana is reachable only through a port-forward, so the message is
the whole of what the person reading it has. Every rule states, in its own
units, the reading and the line it crossed (`observed`) and the first thing to
look at (`check`);
[`message-templates.yml`](../../deploy/grafana/provisioning/alerting/message-templates.yml)
prints those with the series' own labels and the time it began, says "no data"
in words when that is the finding, and leaves out Grafana's bookkeeping labels.
The headline is the rule's own title. Grafana names a rule that read nothing or
could not run `DatasourceNoData` or `DatasourceError`, which says nothing about
which rule it was, so the headline is the rule's title followed by "no data" or
"query failed", and the routing groups by the rule's title so two rules that both
read nothing are two messages rather than one. The text is sent unparsed,
because summaries carry characters Telegram refuses as HTML.

**And a real message goes through the channel every night.** All of the above can
be correct and still reach nobody. That job cannot alert through the channel it
is testing, so the workflow's own result is the signal that survives.

**Every rule says what it watches, and production's rules watch production
alone.** One scrape job reads the production and the staging server, and it
labels each series with the namespace it came from. A rule labelled
`watches: production` reads `namespace="opengate"` on every server series it
holds, so a test driving the staging server is never read as production. A
rule labelled `watches: shared` watches what the two environments share: the
node's disk and memory, and the containers on it.
[`alert-rules.test.sh`](../../scripts/tests/alert-rules.test.sh) holds both
halves.

**A rule reads what it is named for.** The container memory rule reads what the
program holds — its resident memory — against the container's own limit. A
container's working set also counts file cache the kernel takes back on demand,
which put Loki at 93% of its limit while the program held under a third of it,
and this node's memory-pressure and kernel-usage readings are zero, so resident
memory is the one per-program reading it gives. The rule that reports a server
process replaced reads the process's start time itself, because the store counts
a new series' first reading as a change, so a rule counting changes fires
whenever a label is added to the series with the process untouched.

**A test holding the staging claim quiets the shared rules, and only for as
long as it holds it.** The staging deploy, the load run and both drills run on
the node production runs on and drive it hard on purpose, so the shared rules
report the test rather than a fault.
[`alert-quiet-period.sh`](../../scripts/alert-quiet-period.sh) silences
`watches=shared` for the holder of the claim, and
[`staging-lease.sh`](../../scripts/staging-lease.sh) drives it: opened when the
claim is taken, extended to a claim's duration from then at every renewal, and
expired on release whichever way the claim ended. It rides the claim rather than
a clock because a scheduled run starts hours after its cron, and every run that
touches the cluster already takes the claim; the weekly soak and the
performance stack run on a runner of their own and take neither. A run that
dies holding it leaves the node quiet for no longer than its claim could have
held the namespace. Grafana is reached from inside its own pod, with the
password the pod already holds.

**A quiet period that cannot be opened does not fail the run.** The claim, the
measurement and the verdict go ahead; the step prints the refusal as a warning
and writes it into the run's summary. A missed silence announces itself as the
messages it did not hold back, and a night's measurement does not depend on the
alert channel.

## Consequences

A file-provisioned destination is read-only in Grafana's user interface. That is
the point — nobody can silently break it — and it also means an operator cannot
route by hand. Chosen, rather than discovered later. Silences are not
provisioned objects; the one this deployment creates is the quiet period, and
it lives in Grafana's own state, which a pod restart forgets — the next renewal
opens it again.

While a test holds the claim, the shared rules are quiet: a disk or memory
problem on the node that starts during a test is reported when the quiet period
ends, and only if it is still there. Production's own rules stay live
throughout, and none of them sees staging, so a staging server that fails during
a test is reported by that test's verdict rather than by the alert channel.

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
