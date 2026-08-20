# Intel AMT

Some business-class machines carry Intel Active Management Technology — hardware
that answers even when the operating system does not. OpenGate treats AMT as a
property of a managed device, so a technician acts on it from the device's own
page rather than from a separate list.

## What an administrator can do

A device whose hardware supports AMT and whose management engine has dialled in
shows a power-action panel on its detail page: **power on**, **soft off**,
**power cycle** and **hard reset**. These reach the machine's management engine
rather than its operating system, so they work on a host that has hung, crashed
or been powered off. The two destructive ones ask for confirmation before they
are sent. The panel appears only while the AMT connection is live — a machine
that has not dialled in cannot be commanded.

Acting on a device is open to every member of the tenant that owns it, the same
gate the other device commands follow. Which AMT firmware a machine runs, which
model it is and whether AMT is available at all are part of its hardware
inventory, in [Fleet and Devices](./Fleet-and-Devices.md).

## Where the rest of it lives

- The endpoint and the join key that resolves an AMT identity to a managed
  device: [API Reference](../architecture/API-Reference.md#intel-amt).
- The record a CIRA connection maintains and how it is enriched:
  [Database](../architecture/Database.md#amt-devices-table).
- How a device dials in — the CIRA connection, the APF handshake and the port
  forwarding that carries WSMAN — is transport, and lives in
  [Overview](../architecture/Overview.md#intel-amt-management-presence-server-mps).
