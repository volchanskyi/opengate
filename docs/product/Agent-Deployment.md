# Agent Deployment

- [Before you start](#before-you-start)
- [Step 1 — Create an enrollment token](#step-1--create-an-enrollment-token)
- [Step 2 — Install the agent](#step-2--install-the-agent)
- [Step 3 — Confirm it connected](#step-3--confirm-it-connected)
- [What the agent does on the host](#what-the-agent-does-on-the-host)
- [Running the installer again](#running-the-installer-again)
  - [Cloned machines and templates](#cloned-machines-and-templates)
- [Removing a machine](#removing-a-machine)
- [Related](#related)

How a machine joins the fleet: create an enrollment token, run one command on the
target host, and the device appears in the device list.

## Before you start

| Requirement | Detail |
|---|---|
| Operating system | Linux with systemd |
| Architecture | `x86_64` (amd64) or `aarch64` (arm64) |
| Privileges | `root` on the target machine (`sudo`) |
| Tools on the host | `curl` and `systemctl` |
| Outbound network | The host must reach the OpenGate server over HTTPS and over the QUIC control port; **no inbound rule and no VPN is needed** |
| Console permission | Creating an enrollment token is an administrator action |

## Step 1 — Create an enrollment token

1. Open **Add Device** (`/setup`).
2. Under **Enrollment tokens**, create one with:
   - a **label**, so you can tell later what it was for;
   - a **use cap** — how many machines may enrol with it;
   - an **expiry**.
3. The token value can be revealed and copied, or masked again.

Tokens are listed with their use count and expiry. A token that has expired or
exhausted its uses stops working; inactive tokens can be cleaned up in bulk from
[Agent Updates settings](./Agent-Updates.md#enrollment-tokens).

> **Important:** An enrollment token authorises a machine to join the tenant.
> Scope it tightly — a low use cap and a short expiry — and delete it when the
> rollout is done.

## Step 2 — Install the agent

The **Quick setup** panel on the Add Device page shows a ready-made command with
an active token already filled in. Copy it and run it on the target machine:

```bash
curl -sL https://<your-server>/api/v1/server/install.sh | sudo bash -s -- <ENROLLMENT_TOKEN>
```

The installer:

1. checks that it is running as root and that `curl` and systemd are present;
2. validates the enrollment token against the server before downloading anything;
3. detects the OS and architecture and resolves the matching agent build;
4. verifies the download against its published SHA-256 hash;
5. installs the binary, creates the config and data directories, and writes a
   systemd unit;
6. leaves the token for the agent in a one-time install note, readable by root
   alone, which the agent deletes once it knows who it is — the token is never
   written into the service;
7. enables the service and restarts it, so running the command again replaces a
   running agent with the build it just installed.

| Item | Default location |
|---|---|
| Binary | `/usr/local/bin/mesh-agent` |
| Configuration | `/etc/opengate-agent/` |
| Data (identity, local metric history) | `/var/lib/opengate-agent/` |
| systemd service | `mesh-agent.service` |

Each location can be overridden with an environment variable when running the
installer; see the script itself, served at `/api/v1/server/install.sh`.

## Step 3 — Confirm it connected

On the machine:

```bash
systemctl status mesh-agent
```

In the console, the device appears in the device list, filed under the site the
enrollment resolved to, with an **Online** badge. From that point you can open a
session, read its telemetry, and push updates to it.

## What the agent does on the host

- Runs as a systemd service and reconnects on its own after a network outage or a
  reboot.
- **Dials out** to the server and keeps that connection open. It listens on no
  port, so there is nothing inbound to firewall.
- Obtains its own signed identity at first enrollment and reuses it on every
  later start — the enrollment token is spent once. The server chooses the
  device's number, so a token can only ever add a new device.
- Renews its certificate on its own over its live connection: a certificate lasts
  90 days and is renewed from day 60, with nothing to schedule. A machine left
  switched off past its certificate's expiry is let in only to renew, and one off
  for more than about four to six months is refused and needs reinstalling.
- Samples the host's vitals and keeps its own high-resolution history locally, so
  an offline stretch loses nothing ([Device Health](./Device-Health.md)).
- Evaluates detection rules locally and raises alerts that carry their own
  evidence ([Alerts and Rules](./Alerts-and-Rules.md)).
- Serves remote sessions on request ([Remote Sessions](./Remote-Sessions.md)).
- Updates itself when an administrator pushes a signed build
  ([Agent Updates](./Agent-Updates.md)).

## Running the installer again

Running the install command on a machine that already has the agent repairs it
and keeps it the same device: same number, same history, and the token's use
count unchanged. The agent starts over as a new device on its own only when:

- its device was deleted in the console — it wipes its own data, joins with the
  new token, and appears as a new device; the old number stays deleted;
- the command points at a different OpenGate server;
- the command carries `--fresh`.

A token the server refuses leaves the machine as it was, and the agent's log
gives the server's reason.

### Cloned machines and templates

A machine cloned from another, or restored from a snapshot, holds the original's
identity. Until one of them renews, the newest to connect takes the device and
its page warns that the identity is in use on two machines
([Fleet and Devices](./Fleet-and-Devices.md#identity-in-use-on-two-machines));
after a renewal every copy but the one that renewed is refused. Give each copy
its own identity by installing with `--fresh`:

```bash
curl -sL https://<your-server>/api/v1/server/install.sh | sudo bash -s -- --fresh <ENROLLMENT_TOKEN>
```

Install with `--fresh` on every machine built from a template image, or leave
the agent out of the template and install it on first boot.

## Removing a machine

Deleting a device from its detail page erases the machine's centralized data,
and the agent removes itself from the machine — at once if it is online,
otherwise the next time it connects. There is no undo — see
[Data Erasure](./Data-Erasure.md).

## Related

- [Intel AMT](./Intel-AMT.md) — enabling out-of-band power control on capable hardware
- [Tenancy and Access](./Tenancy-and-Access.md) — which customer and site a device lands in
- [Agent Updates](./Agent-Updates.md) — keeping agents current
