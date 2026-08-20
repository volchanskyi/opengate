# Intel AMT

Some business-class machines carry Intel Active Management Technology — hardware
that answers even when the operating system does not. OpenGate treats AMT as a
property of a managed device, so a technician acts on it from the device's own
page rather than from a separate list.

## What it gives you

A device whose hardware supports AMT, and whose management engine has dialled in,
shows a power-action panel on its detail page:

| Action | Effect |
|---|---|
| **Power on** | Powers up a machine that is off |
| **Soft off** | Asks the machine to shut down |
| **Power cycle** | Off, then on |
| **Hard reset** | Immediate reset |

These reach the machine's **management engine**, not its operating system, so
they work on a host that has hung, crashed, or been powered off. The two
destructive actions ask for confirmation before they are sent.

> The panel appears only while the AMT connection is live. A machine that has not
> dialled in cannot be commanded, and the badge beside the hostname is what tells
> you AMT exists on that machine at all.

Acting on a device is open to every member of the tenant that owns it — the same
gate the other device commands follow.

## Enabling AMT on a machine

AMT is enabled in the machine's firmware, not from OpenGate. The steps are the
same everywhere, and the console keeps them on the **Add Device** page (`/setup`)
under *Intel AMT setup*:

1. **Enable AMT in BIOS/UEFI** — enter firmware setup (usually F2 or Del at boot)
   and enable Intel AMT / ME (Management Engine).
2. **Configure MEBx** — press Ctrl+P at boot to enter MEBx. Set a strong password
   and configure the network settings (DHCP or a static address).
3. **Enable remote access** — in MEBx, enable *Remote Setup And Configuration* and
   make sure the AMT network interface is active.
4. **Verify** — the device registers with OpenGate on its own once AMT is
   configured and the network is reachable. The power actions appear on the device
   page once the connection is up.

> Requires Intel vPro-compatible hardware with AMT firmware. Not all Intel
> processors support AMT.

## What you can see about AMT

Which AMT firmware a machine runs, which model it is, and whether AMT is
available at all are part of the machine's hardware inventory — see
[Fleet and Devices](./Fleet-and-Devices.md).

## Related

- [Agent Deployment](./Agent-Deployment.md) — the same setup page, for the agent itself
- [Remote Sessions](./Remote-Sessions.md) — taking over a machine whose OS is running
- [System Architecture](../architecture/System-Architecture.md#intel-amt-management-presence-server-mps) — how a device dials in
