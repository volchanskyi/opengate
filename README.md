<h1 align="center">OpenGate</h1>

<h3 align="center">Remote management for machines that tell you what went wrong.</h3>

<p align="center">
OpenGate is a browser-based platform for managing a fleet of customer machines: see them, take one over, and get told when something breaks — with the evidence already attached.
</p>

<!-- Badges track `dev` because that is the only branch CI runs on: per
     .claude/rules/git.md all work lands on dev; main only receives `[skip ci]`
     auto-merge commits, so a default-branch badge would freeze on whatever ran
     last on main. -->
<p align="center">
  <a href="https://github.com/volchanskyi/opengate/actions/workflows/ci.yml?query=branch%3Adev"><img alt="CI" src="https://github.com/volchanskyi/opengate/actions/workflows/ci.yml/badge.svg?branch=dev"></a>
  <a href="https://github.com/volchanskyi/opengate/actions/workflows/ci.yml?query=branch%3Adev"><img alt="Go Server Coverage" src="https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-coverage.json"></a>
  <a href="https://github.com/volchanskyi/opengate/actions/workflows/ci.yml?query=branch%3Adev"><img alt="Rust Agent Coverage" src="https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-rust-coverage.json"></a>
  <a href="https://github.com/volchanskyi/opengate/actions/workflows/ci.yml?query=branch%3Adev"><img alt="Web Client Coverage" src="https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/volchanskyi/cf505c74b56eab52c9497af517b53222/raw/opengate-web-coverage.json"></a>
</p>

<p align="center">
  <a href="#the-problem-it-solves">The Problem It Solves</a> |
  <a href="#what-you-can-do">What You Can Do</a> |
  <a href="#why-it-is-built-this-way">Why It Is Built This Way</a> |
  <a href="#documentation">Documentation</a>
</p>

---

## The Problem It Solves

At 02:41 a driver rollout goes wrong across forty of Contoso's machines. By
morning the technician on call has 312 alerts sitting in an inbox, each naming
one machine and one threshold, none of them saying that they are all the same
event. Nobody reads 312 alerts. So nobody reads any of them, and the one alert
that mattered — the file server whose disk has been slowing down for a fortnight
— is somewhere in the middle of the pile.

OpenGate answers that with one room saying *forty machines, since 02:41*, sitting
in a queue a person can actually work. The alert that opened the room already
carries what the machine saw when it fired: which of its readings broke pattern,
the seconds around the event, what was running, and the log lines that go with
it. Nobody has to go back and ask a machine what happened an hour ago — which is
just as well, because by then it no longer knows.

## What You Can Do

- **See the fleet.** Every customer's machines, which are up, what each one is
  made of and what it is running, filtered to one customer or across the whole
  tenant.
- **Take a machine over in the browser.** Its screen, a shell, its filesystem and
  a chat window back to whoever is sitting in front of it — no VPN, no inbound
  firewall rule, no client to install on your side.
- **Get told what broke, and why.** A machine watches itself against curated
  detection rules and raises an alert carrying its own evidence.
- **Work a queue instead of an inbox.** Related alerts fold into one incident
  with a status, an owner, a history and a cause code when it is closed.
- **Tune detection per customer.** Retune the thresholds a rule declares
  adjustable, aim them with labels that cut across sites, pace how far a new rule
  reaches, and stop one outright without waiting for a release.
- **Quiet a machine during host work.** Maintenance mode stops the noise a
  planned reboot would otherwise generate, without making the machine look dead.
- **Run several customers from one console.** Customers, sites, security groups
  and an audit trail, with each tenant's data walled off in the database itself.
- **Erase a device's data on request.** A deletion is a real erasure across every
  store, not a hidden flag.
- **Reach a machine that will not boot.** On hardware that supports Intel AMT,
  power it on, cycle it or reset it below the operating system.

## Why It Is Built This Way

- **Machines dial out, so nothing has to be exposed inbound.** A managed device
  opens the connection to the server and keeps it; there is no listening port for
  anyone to find, and no firewall change to ask a customer for.
- **The device does its own analysis, so the network carries summaries.** A
  machine keeps its own high-resolution history locally and sends a small fixed
  set of readings — so the cost of watching a fleet is bounded by how many
  machines there are, not by how closely each one is watched.
- **An alert arrives already carrying its evidence.** Whatever explains a finding
  is attached at the moment it fires. There is no follow-up question to a machine
  that has since rebooted, and no gap between what was seen and what was recorded.

## Documentation

Start at the [documentation index](./docs/Home.md). It is three trees:

| Tree | What is in it |
|---|---|
| [Product](./docs/product/) | What the system does — the fleet, remote sessions, device health, alerts and rules, investigations, tenancy, erasure |
| [Architecture](./docs/architecture/) | How it is built — components, connection model, wire protocol, REST API, database schema |
| [Infrastructure](./docs/infrastructure/) | How it runs — the Kubernetes cluster, Terraform, CI/CD, observability, testing |

The REST API is also published as a [browsable reference](https://volchanskyi.github.io/opengate/docs/api/).
