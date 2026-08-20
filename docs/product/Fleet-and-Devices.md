# Fleet and Devices

The fleet views answer the questions a technician asks first: what machines does
this customer have, which of them are up, what is each one made of, and what is
wrong with it right now.

## Screens

| Screen | Path | What it is for |
|---|---|---|
| Dashboard | `/` | Fleet counts and health for the selected customer, each tile a link into a filtered device list |
| Device list | `/devices` | Every device, searchable and filterable, with a site sidebar |
| Device detail | `/devices/:id` | One machine: status, incidents, telemetry, inventory, logs and the actions you can take on it |

## Dashboard

Four counts across the top — **Total devices**, **Online**, **Offline** and
**In maintenance** — and a **Fleet health** breakdown underneath. Every tile is
a link that opens the device list already filtered:

| Tile | Opens |
|---|---|
| Total devices | `/devices` |
| Online | `/devices?status=online` |
| Offline | `/devices?status=offline` |
| In maintenance | `/devices?maintenance=true` |
| A fleet-health band | `/devices?health=<band>` |

Fleet health groups devices by the anomaly rate each one reported about itself:

| Band | Meaning |
|---|---|
| **Healthy** | The device's own readings are behaving normally |
| **Watch** | Some readings are breaking pattern |
| **Anomalous** | A large share of readings are breaking pattern |
| **No data** | The device has reported no telemetry — offline, new, or in maintenance |

The bands are coarse on purpose. They tell you where to look; they are not an
alerting signal. What raises alerts is in [Alerts and Rules](./Alerts-and-Rules.md).

> **Note:** Every dashboard tile reads one fixed-size response, so the page costs
> the same whether the fleet is ten machines or ten thousand.

## Selecting a customer

The customer picker in the navigation bar narrows every fleet view to one
customer, or opens it up to the whole tenant.

- The choice applies to the dashboard tiles and the device list at once, so the
  two never describe different sets of machines.
- It survives a page reload, because a technician works one customer at a time
  for long stretches.
- Deleting or retiring a customer clears the selection rather than leaving the
  fleet looking empty.

## Finding a device

- **Search** — filter the list by hostname as you type.
- **Site sidebar** — narrow to one site within the selected customer.
- **URL filters** — `status`, `health` and `maintenance` are query parameters, so
  a filtered view can be bookmarked or pasted to a colleague.

Each device card carries a status badge, a health badge, a maintenance badge when
the machine is in maintenance, and a hint of the machine's discovered footprint
(how many services and containers it is running).

## The device detail page

| Section | What it shows |
|---|---|
| Header | Hostname, online status, health, AMT and maintenance badges |
| Incident strip | The open incidents this machine is caught up in, each a link into its room |
| Telemetry | Anomaly rate and per-family metric charts over a chosen window — see [Device Health](./Device-Health.md) |
| System logs | On-demand pull of the machine's own log — see [Endpoint Logs](./Endpoint-Logs.md) |
| Inventory | Hardware, and the discovered footprint: listening ports, services, database engines, containers and packages |
| Active sessions | Remote sessions currently open against the machine |
| Maintenance | Enter or leave maintenance mode, with a reason |
| AMT power actions | Out-of-band power control, when the hardware supports it — see [Intel AMT](./Intel-AMT.md) |

### Actions

| Action | Who can do it | Notes |
|---|---|---|
| Start a session | Any tenant member | Opens the session view — see [Remote Sessions](./Remote-Sessions.md) |
| Restart the agent | Any tenant member | The service restarts and reconnects; the device stays enrolled |
| Upgrade the agent | Administrator | Offered when a newer build matches the machine's OS and architecture — see [Agent Updates](./Agent-Updates.md) |
| Enter or leave maintenance | Any tenant member | Quiets detection during planned host work |
| File into a site | Administrator | The site must belong to the device's own customer |
| Move to another customer | Administrator | Clears the site filing in the same step, so a machine never arrives at a customer filed under a site that customer does not have |
| Delete the device | Administrator | An irreversible erasure, behind a confirm step — see [Data Erasure](./Data-Erasure.md) |

## Hardware inventory and discovered footprint

The agent collects the machine's hardware, what it is listening on, and what it
is running; the device page reads it back and sorts it.

| Group | Contents |
|---|---|
| Hardware | CPU, memory, disks, network interfaces, firmware, and whether Intel AMT is present |
| Listening ports | Open TCP and UDP ports and the process behind each |
| Services | Installed and running system services |
| Database engines | Database software detected on the host |
| Containers | Container runtimes and running containers |
| Packages | Installed software packages |
| Processes | Point-in-time process snapshots |

The tables that hold these, their retention and their tenancy rules are in
[Database](../architecture/Database.md#device-hardware-table); the endpoints are in
[API Reference](../architecture/API-Reference.md#device-inventory).

## Browser notifications

A technician who grants the browser permission is told about four events:

| Event | Fires when |
|---|---|
| `device_online` | A machine reconnects |
| `device_offline` | A machine stops reporting |
| `session_started` | A remote session opens |
| `session_ended` | A remote session closes |

That is the whole set — **alerts never travel this way**. An alert opens or joins
an incident and is worked from the triage queue in
[Investigations](./Investigations.md).

Push delivery itself — the signing keys and the service worker that handles a push
and its click-through — is in [System Architecture](../architecture/System-Architecture.md#notifications).

## Console behaviour

- **Breadcrumbs** on every page show where you are and let you climb back.
- **Toasts** confirm that an action succeeded or failed, and are announced to
  screen readers.
- **Crash recovery** — a render failure shows a recovery panel rather than a
  blank page.
- **Pages load on demand**, so the console starts fast and a rarely used screen
  costs nothing until it is opened.

## Related

- [Remote Sessions](./Remote-Sessions.md) — taking a machine over
- [Device Health](./Device-Health.md) — what the readings mean
- [Tenancy and Access](./Tenancy-and-Access.md) — who may see which machines
- [Agent Deployment](./Agent-Deployment.md) — getting a machine into the fleet
