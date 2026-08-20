<h1 align="center">OpenGate</h1>

<h3 align="center">Fleet remote monitoring and management system.</h3>

<p align="center">
Monitored machines
are running the agents that analyse local system anomalies and raise alerts with the evidence.
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
  <a href="#what-it-does">What it does</a> |
  <a href="#network-model">Network model</a> |
  <a href="#deploying-the-server">Deploying the server</a> |
  <a href="#adding-a-machine">Adding a machine</a> |
  <a href="#security-posture">Security posture</a> |
  <a href="#documentation">Documentation</a>
</p>

---

## What it does

OpenGate manages fleets of endpoints from a centralized web-based UI: Asses fleet`s health, take over individual systems and investigate endpoints.

| Capability | Features |
|---|---|
| **Fleet visibility** | Individual machines, which are up, hardware configurations, software inventory, system and network configuration |
| **Remote control** | Screen, shell, file transfer and chat in the browser. |
| **Health monitoring** | A set of vitals per machine, including kernel pressure (how long work actually waited) and disk service time |
| **Detection** | Curated rules evaluated **on the host**, against anomalies and host's own log records |
| **Triage** | Related alerts fold into one incident with a status, an owner, a history and a cause code.|
| **Out-of-band control** | Power on, cycle or reset AMT-capable hosts bypassing operation system controls |
| **Multi-customer operation** | Hosts can be assigned to individual customer sites and grouped by custom operational domains |
| **Over-the-air agent updates** | Secure agent updates with automatic rollback |
| **Erasure on request** | Deleting a device or a customer is a secure erasure across every store, with a retained audit trail |

## Network model


| Leg | Direction | Transport | Default port |
|---|---|---|---|
| Agent → server | **Outbound from the managed machine** | QUIC with mutual TLS | UDP 9090 |
| Browser → server | Outbound from the technician | HTTPS + WebSocket | TCP 443 via ingress (server listens on 8080) |
| AMT hardware → server | Outbound from the managed machine's management engine | TLS (CIRA) | TCP 4433 |

What that means in practice:

- **No inbound rule on the customer network.** Managed machines open the
  connection and keep it. There is no listening port on the agent for anyone to
  find, and no NAT traversal to configure.
- **No VPN and no jump host** for remote sessions. A session rides the existing
  connection through the server's relay, and upgrades itself to a direct
  peer-to-peer path when both networks allow it.
- **Egress is all a customer firewall needs to allow**: the QUIC port to your
  server, plus HTTPS for enrollment and binary download. Ports are configurable.

### What it costs to watch a fleet

Monitored hosts keep their own high-resolution history locally and report a **fixed,
bounded set** of per-minute readings. The central store's cost is set
by how many machines you have — not by how closely each one is watched. Host sampling per second "costs" no more than central sampling per minute.

An offline machine loses nothing: it backfills its own history on reconnect, at a
rate the server grants, so a fleet coming back at once cannot swamp ingest.

## Deploying the server

| Component | Requirement |
|---|---|
| Server | A single Go binary, published as a container image |
| Database | PostgreSQL 17 — migrations run on startup |
| Metrics store | VictoriaMetrics |
| Web console | A static SPA, served by the server or by your ingress |
| TLS | Terminated at the ingress for HTTP; the QUIC and AMT listeners carry their own |

The supported deployment is Kubernetes via the Helm chart +
L4 services that expose the QUIC and AMT listeners.

## Adding a machine

A shell-script installer validates the token before downloading, verifies the binary against
its published hash, installs to `/usr/local/bin`, and writes a systemd unit that
restarts on failure and survives reboots. Details: [Agent Deployment](docs/product/Agent-Deployment.md).

## Security posture

| Control | How it works |
|---|---|
| Device identity | Each agent enrols once with a scoped, expiring token and receives its own signed certificate. All later connections are mutually authenticated |
| Tenant isolation | Enforced in the database itself, not only in application code |
| Log privacy | Raw log lines are never centralized. They are pulled on demand, security-sensitive info is pre-redacted on the machine and on the server |
| Audited access | operator actions and rule change are logged with the actor and date/time |
| Agent updates | Hash **and** signature verified on the machine before installation, with an automatic rollback watchdog |
| Blast-radius control | A new detection rule reaches an estate in stages It can be stopped automatically if the estate gets noisy or manually killed for an individual customer or all customers under the tenant |
| Alert budgets | Per-machine and per-customer hourly ceilings, enforced on the machine as well as the server, so one looping host cannot drown the fleet's detection |
| Erasure | Deletions are real erasures across every store, with a permanent deny-list so purged data cannot be recreated |

## Documentation

Start at the [documentation index](docs/Home.md).

| Tree | For |
|---|---|
| [Product](docs/product/) | Operators and technicians — what the system does and how to use it |
| [Architecture](docs/architecture/) | How it is built: components, connection model, wire protocol, REST API, database schema |
| [Infrastructure](docs/infrastructure/) | How it runs: Kubernetes, Terraform, CI/CD, observability, testing |

The REST API is also published as a [browsable
reference](https://volchanskyi.github.io/opengate/docs/api/).

## Repository layout

| Path | Contents |
|---|---|
| [`agent/`](agent/) | The Rust agent that runs on managed machines |
| [`server/`](server/) | The Go server: REST API, agent transport, relay, detection, lifecycle |
| [`web/`](web/) | The React-based Web Console |
| [`api/`](api/) | The OpenAPI specification |
| [`deploy/`](deploy/) | Helm charts, Terraform, and test topologies |

