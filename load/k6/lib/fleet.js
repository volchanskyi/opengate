// Interprets a fleet read: an empty list is nothing enrolled, an all-offline list a restart.
// The server sets every online device offline on start, so a restart under load empties the fleet.

/** The status a machine carries while the server is holding its connection. */
const ONLINE = "online";

// An absent or non-list body yields no machines, so setup reports what it read.
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
