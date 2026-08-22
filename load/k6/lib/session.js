// Session helpers shared by the k6 scenarios.
//
// The scenarios drive the API as an ordinary organization member, which is what
// a load generator can be: organization is the visibility boundary, so a member
// reads the whole fleet, while creating or deleting a site is administrator
// work the server refuses. A scenario that stands up its own fixtures would get
// a 403 and then measure the error path, so the scenarios read the fleet the
// staging organization already has.
//
// Every identity a run creates carries the marker below, and every run writes
// down what it created. A load run that cannot say what it made cannot remove
// it, and residue accumulates one uncleaned run at a time until every account
// in the environment belongs to a load test.
import http from "k6/http";

/**
 * The marker every load-test identity carries, in its local part and its
 * domain. Cleanup selects on it, so nothing a run creates is anonymous.
 *
 * The domain is `.invalid`, which is reserved by RFC 2606 and resolves nowhere
 * — a run cannot accidentally send mail to a real address.
 */
export const LOAD_TEST_MARKER = "opengate-loadtest";

/** Password every load-test identity is created with. */
const LOAD_TEST_PASSWORD = "LoadTestPass123!";

/** Authorization headers for a bearer token. */
export function authHeaders(token) {
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  };
}

/**
 * The email one load-test identity uses. It is built from the run id rather
 * than the clock, so a cleanup pass run after the fact can name exactly what
 * this run created rather than guessing from a timestamp window.
 */
export function loadTestEmail(prefix, runId, vu) {
  return `${LOAD_TEST_MARKER}-${runId}-${prefix}-${vu}@${LOAD_TEST_MARKER}.invalid`;
}

/**
 * The run id this scenario belongs to. CI supplies it; a local run gets a
 * stable stand-in so its identities are still recognisable and still removable.
 */
export function runId() {
  return __ENV.LOADTEST_RUN_ID || "local";
}

/**
 * Register a throwaway member of the staging organization and return its token
 * alongside the email it was created under, so the caller can write the email
 * into the run's cleanup manifest.
 *
 * Throws on any unexpected status: a load run against an unusable session
 * measures nothing, and failing here names the real cause instead of leaving
 * every request in the run to fail for a reason the summary cannot show.
 */
export function registerMember(baseUrl, prefix) {
  const email = loadTestEmail(prefix, runId(), __VU);
  const resp = http.post(
    `${baseUrl}/api/v1/auth/register`,
    JSON.stringify({ email, password: LOAD_TEST_PASSWORD }),
    { headers: { "Content-Type": "application/json" } }
  );
  if (resp.status !== 201) {
    throw new Error(`setup: register returned ${resp.status}: ${resp.body}`);
  }
  const token = resp.json("token");
  if (!token) {
    throw new Error("setup: register returned no token");
  }
  return { token, email };
}

/**
 * Ids of the sites visible to this member, newest last. Empty when the
 * organization has no sites — a valid fleet shape, not a setup failure.
 */
export function visibleSiteIds(baseUrl, token) {
  const resp = http.get(`${baseUrl}/api/v1/sites`, { headers: authHeaders(token) });
  if (resp.status !== 200) {
    throw new Error(`setup: list sites returned ${resp.status}: ${resp.body}`);
  }
  return (resp.json() || []).map((site) => site.id);
}

/**
 * Device-list URL, narrowed to `siteId` when one is given. An absent site id
 * means the organization has no sites, so the unfiltered fleet read is the
 * request a client would actually make.
 */
export function devicesUrl(baseUrl, siteId) {
  return siteId
    ? `${baseUrl}/api/v1/devices?site_id=${siteId}`
    : `${baseUrl}/api/v1/devices`;
}

/**
 * Ids of the devices currently online. A session can only be opened against a
 * machine that is connected, so a scenario that needs one asks for the fleet
 * and keeps the ones that answer.
 */
export function onlineDeviceIds(baseUrl, token) {
  const resp = http.get(devicesUrl(baseUrl), { headers: authHeaders(token) });
  if (resp.status !== 200) {
    throw new Error(`setup: list devices returned ${resp.status}: ${resp.body}`);
  }
  return (resp.json() || [])
    .filter((device) => device.status === "online")
    .map((device) => device.id);
}

/**
 * What this run created, in the shape the cleanup pass reads. It is printed at
 * the end of a scenario so the workflow can collect it, because a manifest that
 * only exists in the generator's memory is a manifest nobody can act on when
 * the generator is a pod that has already been deleted.
 */
export function cleanupManifest(emails) {
  return {
    marker: LOAD_TEST_MARKER,
    run_id: runId(),
    users: emails,
  };
}

/** Print the manifest on its own line, prefixed so a log scrape can find it. */
export function printCleanupManifest(emails) {
  console.log(`LOADTEST_CLEANUP_MANIFEST ${JSON.stringify(cleanupManifest(emails))}`);
}
