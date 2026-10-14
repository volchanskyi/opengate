# Remote Sessions

- [What you can do in a session](#what-you-can-do-in-a-session)
- [Opening a session](#opening-a-session)
- [One technician per machine](#one-technician-per-machine)
- [How a session ends](#how-a-session-ends)
- [Which tabs appear](#which-tabs-appear)
- [How the connection works](#how-the-connection-works)
  - [What each side learns](#what-each-side-learns)
  - [On a poor network](#on-a-poor-network)
- [Files and downloads](#files-and-downloads)
- [Session notifications](#session-notifications)
- [Related](#related)

Take a machine over from the browser: its screen, a shell, its filesystem, and a
chat window back to whoever is sitting in front of it. Nothing is installed on
the technician's side, and nothing has to be opened inbound on the customer's
network.

## What you can do in a session

| Tab | What it gives you |
|---|---|
| **Desktop** | The machine's screen, with your mouse and keyboard forwarded to it |
| **Terminal** | A real shell on the host, in the browser |
| **Files** | Browse directories, download files into your browser's Downloads, and view a file of up to 1 MiB in place |
| **Chat** | A message window to the person sitting at the machine |

Every tab stays open while you switch between them, so a download keeps running
while you type in the terminal.

## Opening a session

1. Open the device from the device list.
2. Select **Open session**.
3. A new browser tab opens at `/devices/<id>/session`, with the tabs that
   machine supports.

The session tab stands on its own. Its bar and its browser-tab title name the
machine and its customer — "FS01 — Contoso" — and it carries none of the fleet
navigation. Choosing another customer in a fleet tab changes only what that
tab's lists show; the session carries on untouched. Keep the fleet dashboard in
one tab and the customer's file server in another.

Only members of the tenant that owns the device can open a session on it; see
[Tenancy and Access](./Tenancy-and-Access.md).

## One technician per machine

A machine takes one session at a time. While someone holds it, the device page
and its row in the fleet list read **Session open — Alice, since 14:02**, and
for everyone else **Open session** is switched off. A technician reads "an
administrator can end it"; an administrator also gets **End session**, which
asks before it acts. Two technicians clicking at the same moment get one session
between them, and the other is told who has it.

For the technician who holds the machine, the button changes:

- **Go to session** brings their session tab forward without reloading it.
  Where the browser or the desktop will not switch to another tab, the page
  says "Open in another tab of this browser".
- **Move session here** appears where this browser holds no tab for the
  machine — another computer, or a browser that was restarted — and moves the
  session to this one.

## How a session ends

| What happens | What the session tab shows |
|---|---|
| You close the session tab | The session ends |
| You reload the session tab | A new session replaces the old one |
| You open the machine again somewhere else | The old tab reads **Opened elsewhere** |
| You, or an administrator, end the session from the device page | The tab reads **Ended by <name>** |
| You sign out in any tab of this browser | Every session tab of this browser ends, reading **Signed out** |
| The connection is lost and does not come back | **Connection lost — reconnect** |

Signing out at the end of the day closes the root terminal on the customer's
server in the other tab with it.

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

The session then tries once to move onto a **direct link** between your browser
and the machine, which cuts the wait on every keystroke: a technician in the
same building as the machine waits about a millisecond instead of two trips to
the server. The session bar shows **Direct** or **Through server**; hovering over
Through server gives the reason in plain words:

| Reason | What it means |
|---|---|
| looking for a direct route | The session has just started and is trying |
| no direct route on this network | A router or firewall on one side lets no direct link through |
| this browser can't | The browser offers no way to make a direct link |
| the direct link dropped | The link was lost, and the session moved back to the server |

Nothing waits for the attempt: the session is usable through the server from the
first moment, and moving between the two paths loses, repeats and reorders
nothing.

Typing stays quick while a download runs: on a 40 ms connection your keystrokes
echo within a third of a second at 10 Mbit/s and within a tenth of a second at
100 Mbit/s, on either path. A folder of 120 000 files fills in as its pages
arrive, and typing is not held up while it does.

### What each side learns

Your browser learns the machine's addresses on its own network, which the
machine's hardware inventory already shows, and its public address. The machine
learns your public address — often a home address, and, if you use a VPN that
sends only some traffic through it, or a web proxy, an address your web browsing
would not show. Every session tries the direct link. Logs record only what kind
of address was used, never the address.

### On a poor network

- A drop shorter than 3 seconds changes nothing: the session stays Direct and
  every keystroke arrives, late and in order.
- A longer drop moves the session to the server once, and it stays there until
  you close it. On a train, the dropouts between masts leave the session Direct;
  a tunnel moves it to the server, where it stays.
- A command you typed as the link dropped runs exactly as typed — `rm -rf
  /tmp/build` never loses the end of its path before the Enter.

## Files and downloads

A download goes to your browser's Downloads like any other file, with its
progress there and on the session tab's Transfers strip. The file is never held
whole in memory, on the machine or in the tab, so a 2 GB log bundle downloads
the same way as a small one.

- **One at a time.** Ask for a second download, or a View, while one runs, and
  it waits on the Transfers strip — "Waiting: a.log, b.log" — then runs.
- **Keep the session tab open** until the download finishes. Closing it ends the
  session and the download with it. A download carries on across a move between
  Direct and the server.
- **A file the machine cannot read** ends with a message, and the next download
  starts.
- **View** opens a file of up to 1 MiB in place; a larger one says to download
  it instead.
- A browser that cannot save through its background worker can save files of up
  to 64 MiB from a session, and says so for a larger one.

## Session notifications

A session starting and ending are two of the four events browser push carries;
see [Fleet and Devices](./Fleet-and-Devices.md#browser-notifications).

## Related

- [Fleet and Devices](./Fleet-and-Devices.md) — finding the machine to work on
- [Endpoint Logs](./Endpoint-Logs.md) — reading a machine's log without opening a session
- [Intel AMT](./Intel-AMT.md) — reaching a machine whose operating system is not running
- [System Architecture](../architecture/System-Architecture.md#session-lifecycle) — how a session is set up, the relay, the direct link and teardown; the frames are in [Wire Protocol](../architecture/Wire-Protocol.md), and the decisions in [ADR-127](../adr/ADR-127-direct-link.md)
