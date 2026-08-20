# Fleet and Devices

The fleet views are where a technician spends most of their time: what machines a
customer has, which of them are up, what each one is made of, what it is running,
and which incidents it is currently caught up in.

Remote control of a machine is in [Remote Sessions](./Remote-Sessions.md); its
readings are in [Device Health](./Device-Health.md); who may see which customer's
machines is in [Tenancy and Access](./Tenancy-and-Access.md).

## Fleet views

| Feature | Path | Description |
|---------|------|-------------|
| **Dashboard** | `/` | Landing page — overview of the fleet for the selected customer |
| **Device List** | `/devices` | Device listing with search/filter, site sidebar, per-card discovered-footprint hint (service/container counts) |
| **Device Detail** | `/devices/:id` | Device info, a strip of the open incidents the machine is caught up in, AMT power actions, filing into a site, move to another customer, hardware inventory, discovered footprint (sortable ports/services/DB engines/containers/packages), on-demand device logs, agent restart |

## What the console gives every page

- **ErrorBoundary**: Top-level `<ErrorBoundary>` wraps the app for crash resilience — catches render errors and displays a recovery UI instead of a blank screen
- **Lazy loading**: All feature pages use `React.lazy` + `<Suspense>` for code-splitting, producing 16+ chunks that load on demand
- **Breadcrumbs**: Context-aware breadcrumb navigation rendered on every page via `<Breadcrumbs />`
- **Toast notifications**: Global toast system (`useToast` hook + `<ToastContainer />`) for success/error feedback. Toast IDs use `crypto.randomUUID()` for uniqueness. The toast container has `aria-live` for screen reader accessibility
- **Device search/filter**: Inline search on the device list page to filter devices by hostname
- **Customer picker**: A navbar control that narrows every fleet view to one customer, or to the whole tenant. It publishes the choice and nothing else — the dashboard tiles and the device list re-read on a change, so the two never describe different sets — and remembers it across a reload, since a technician works one customer at a time for stretches. A customer that is deleted or retired away stops being the selection rather than leaving the fleet looking empty

## What a machine is made of, and what it is running

A device's hardware inventory, its discovered footprint (listening ports,
services, database engines, containers, packages) and its process snapshots are
collected by the agent and read back on the device-detail page. The tables that
hold them, their retention and their tenancy predicates are in
[Database](../architecture/Database.md#device-hardware-table); the endpoints are in
[API Reference](../architecture/API-Reference.md#device-inventory).

## Browser notifications

A technician who has granted the browser permission is told when a machine comes
online or goes offline, and when a remote session starts or ends — `device_online`,
`device_offline`, `session_started`, `session_ended`. That is the whole event set:
alerts do not travel this way. An alert opens or joins an incident and is worked
from the queue, in [Investigations](./Investigations.md).

Push delivery — VAPID keys, the `Notifier` seam, and the service worker that
handles a push event and its click-through — is in
[Overview](../architecture/Overview.md#notifications).
