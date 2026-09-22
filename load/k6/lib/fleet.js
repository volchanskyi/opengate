// What a fleet read came back with, and which of two very different things an
// empty answer means.
//
// The relay scenario opens sessions against machines the machine-side harness is
// holding connected, so a read with no online machine in it stops the run. It
// stopped it with one message for two situations that need entirely different
// work: a fleet that never arrived, and a fleet that arrived and that the server
// is no longer holding. The server sets every online device offline when it
// starts, so a restart under the run's own load produces the second while the
// machines stay connected throughout.
//
// The answer was always in the list the read had just fetched — an empty list is
// nothing ever enrolled, a list of machines none of which is online is the
// server having forgotten them. This says so.
//
// It is a module of its own because it decides rather than asks: everything it
// needs is the list, so it can be driven with real fleet shapes by
// scripts/tests/loadtest-fleet-read.test.sh.

/** The status a machine carries while the server is holding its connection. */
const ONLINE = "online";

/**
 * The machines in a fleet read, whatever the server answered with. A body that
 * is absent or is not a list is no machine rather than a throw, so a scenario
 * reports what it read instead of dying inside its own setup.
 */
function machines(fleet) {
  return Array.isArray(fleet) ? fleet : [];
}

/** Ids of the machines the read found connected. */
export function onlineIds(fleet) {
  return machines(fleet)
    .filter((device) => device && device.status === ONLINE)
    .map((device) => device.id);
}

/**
 * Why this read offers no machine to open a session against, or null when it
 * offers one.
 *
 * The two answers name different work. Nothing enrolled is the machine side
 * never having arrived — the harness, the enrolment path, or the order the two
 * were started in. Machines present and none of them connected is the server's
 * own record of the fleet having been emptied, which is what it writes on start.
 */
export function emptyFleetReason(fleet) {
  const found = machines(fleet);
  if (onlineIds(found).length > 0) {
    return null;
  }
  if (found.length === 0) {
    return (
      "the fleet read came back with no machine at all: nothing has enrolled against this server, " +
      "so the machine-side harness never arrived"
    );
  }
  return (
    `the fleet read came back with ${found.length} machine(s) and none of them online: ` +
    "the server is holding no connection, which is what its record looks like after a restart — " +
    "it sets every online machine offline when it starts, and the fleet returns only as each one re-registers"
  );
}
