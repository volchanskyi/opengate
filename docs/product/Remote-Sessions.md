# Remote Sessions

Take a machine over from the browser: its screen, a shell, its filesystem, and a
chat window back to whoever is sitting in front of it. Nothing is installed on
the technician's side, and nothing has to be opened inbound on the customer's
network.

## What you can do in a session

| Tab | What it gives you |
|---|---|
| **Desktop** | The machine's screen, with your mouse and keyboard forwarded to it |
| **Terminal** | A real shell on the host, in the browser |
| **Files** | Browse directories, download and upload files with progress, and view a file in place |
| **Chat** | A message window to the person sitting at the machine |

All four run over one connection to the machine, opened from `/sessions/:token`.
The toolbar shows the connection state throughout.

## Starting a session

1. Open the device from the device list.
2. Select **Start session**.
3. The session view opens with the tabs that machine supports.

Only members of the tenant that owns the device can open a session on it; see
[Tenancy and Access](./Tenancy-and-Access.md).

Sessions currently open against a machine are listed on its detail page, so you
can see that a colleague is already in there.

## Which tabs appear

Tabs are gated by what the agent reports it can do, so a machine never offers a
control it cannot honour.

| Machine reports | Tabs shown |
|---|---|
| Terminal and file access only (typical Linux server) | Terminal, Files |
| Screen capture and input injection as well | Desktop, Terminal, Files, Chat |

## How the connection works

Every session starts through the server's relay, which works from anywhere the
browser can reach the server — no inbound firewall rule, no VPN, no port forward
on the customer's network.

When the network on both ends allows it, the session then upgrades itself to a
**direct connection** between the browser and the machine, which cuts latency on
the screen and terminal. If the upgrade cannot be completed, the session keeps
running on the relay; nothing is interrupted and there is nothing to retry.

What travels over the connection:

- **Desktop** — screen frames at roughly 10 frames per second, with the newest
  frame always preferred over a late one.
- **Terminal** — a shell on the host, streamed both ways.
- **Files** — directory listings and chunked transfers, permission-gated on the
  machine.
- **Input** — your mouse and keyboard events, injected into the machine's
  desktop.

The establishment sequence, the relay, the direct-connection upgrade and teardown
are in [System Architecture](../architecture/System-Architecture.md#session-lifecycle); the frame
encoding is in [Wire Protocol](../architecture/Wire-Protocol.md).

## Session notifications

A session starting and ending are two of the four events browser push carries;
see [Fleet and Devices](./Fleet-and-Devices.md#browser-notifications).

## Related

- [Fleet and Devices](./Fleet-and-Devices.md) — finding the machine to work on
- [Endpoint Logs](./Endpoint-Logs.md) — reading a machine's log without opening a session
- [Intel AMT](./Intel-AMT.md) — reaching a machine whose operating system is not running
