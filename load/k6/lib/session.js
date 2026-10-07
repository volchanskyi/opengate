// Session helpers shared by the k6 scenarios, which act as an ordinary organization member.
// Every identity a run creates carries the marker below, and every run writes down what it made.
import client from "k6/http";
import { Counter } from "k6/metrics";

// The status the server answers a request it will not serve with.
const REFUSED_STATUS = 429;

// Counts requests the server turned away with 429; a zero is added on every answered request.
// An unincremented k6 counter is missing from the export, which would hide whether anyone counted.
const refused = new Counter("requests_refused");

/**
 * Counts one answer and hands it back unchanged.
 * Exported for a WebSocket upgrade, which k6 opens outside the client below.
 */
export function counted(response) {
  refused.add(response && response.status === REFUSED_STATUS ? 1 : 0);
  return response;
}

/**
 * The request client every scenario uses in place of k6's own, so the count above covers the
 * whole run. The verbs are the ones the scenarios use; a scenario needing another adds it here.
 */
export const http = {
  get: (url, params) => counted(client.get(url, params)),
  post: (url, body, params) => counted(client.post(url, body, params)),
};

/**
 * The marker every load-test identity carries, in its local part and its domain.
 * Cleanup selects on it; the `.invalid` domain (RFC 2606) resolves nowhere.
 */
export const LOAD_TEST_MARKER = "opengate-loadtest";

/** Password every load-test identity is created with. */
const LOAD_TEST_PASSWORD = "LoadTestPass123!";

/**
 * The address one simulated technician presents, from the 131,072-address 198.18.0.0/15
 * benchmarking range (RFC 2544); the server counts requests per address.
 */
export function presentedAddress(index) {
  const offset = Math.abs(index | 0) % 131072;
  return `198.${18 + (offset >> 16)}.${(offset >> 8) & 255}.${offset & 255}`;
}

// One scenario's block size and the block count; their product is the whole address range.
const ADDRESSES_PER_SCENARIO = 8192;
const SCENARIO_BLOCKS = 16;

/**
 * The block of addresses this scenario presents from, derived from its name by hash so that
 * concurrent scenarios do not share an allowance.
 */
export function scenarioBlock(name) {
  let hash = 2166136261;
  for (let i = 0; i < name.length; i++) {
    hash ^= name.charCodeAt(i);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash % SCENARIO_BLOCKS;
}

/** The address this virtual user presents, stable for the life of the run. */
export function technicianAddress() {
  const scenario = __ENV.LOADTEST_SCENARIO || "adhoc";
  return presentedAddress(
    scenarioBlock(scenario) * ADDRESSES_PER_SCENARIO + (__VU % ADDRESSES_PER_SCENARIO)
  );
}

/** Authorization headers for a bearer token, from this technician's address. */
export function authHeaders(token) {
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
    "X-Forwarded-For": technicianAddress(),
  };
}

/** Headers for a request that carries no session, such as a health check. */
export function anonymousHeaders() {
  return { "X-Forwarded-For": technicianAddress() };
}

/** The email one load-test identity uses, built from the run id so cleanup can name it exactly. */
export function loadTestEmail(prefix, runId, vu) {
  return `${LOAD_TEST_MARKER}-${runId}-${prefix}-${vu}@${LOAD_TEST_MARKER}.invalid`;
}

/** The run id this scenario belongs to; a local run uses a stable stand-in. */
export function runId() {
  return __ENV.LOADTEST_RUN_ID || "local";
}

/**
 * Registers a throwaway member of the staging organization and returns its token with the email
 * for the cleanup manifest. Throws on any unexpected status.
 */
export function registerMember(baseUrl, prefix) {
  const email = loadTestEmail(prefix, runId(), __VU);
  const resp = http.post(
    `${baseUrl}/api/v1/auth/register`,
    JSON.stringify({ email, password: LOAD_TEST_PASSWORD }),
    { headers: { "Content-Type": "application/json", "X-Forwarded-For": technicianAddress() } }
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

/** Ids of the sites visible to this member; empty when the organization has none. */
export function visibleSiteIds(baseUrl, token) {
  const resp = http.get(`${baseUrl}/api/v1/sites`, { headers: authHeaders(token) });
  if (resp.status !== 200) {
    throw new Error(`setup: list sites returned ${resp.status}: ${resp.body}`);
  }
  return (resp.json() || []).map((site) => site.id);
}

/**
 * The first site that holds machines, or undefined when none does.
 * `devicesUrl` reads undefined as the unfiltered fleet request.
 */
export function siteWithDevices(baseUrl, token) {
  const headers = authHeaders(token);
  for (const siteId of visibleSiteIds(baseUrl, token)) {
    const resp = http.get(devicesUrl(baseUrl, siteId), { headers });
    if (resp.status !== 200) {
      throw new Error(`setup: list devices returned ${resp.status}: ${resp.body}`);
    }
    if ((resp.json() || []).length > 0) {
      return siteId;
    }
  }
  return undefined;
}

/** Device-list URL, narrowed to `siteId` when one is given. */
export function devicesUrl(baseUrl, siteId) {
  return siteId
    ? `${baseUrl}/api/v1/devices?site_id=${siteId}`
    : `${baseUrl}/api/v1/devices`;
}

/** The whole fleet as the server answered with it; fleet.js decides what an empty answer means. */
export function readFleet(baseUrl, token) {
  const resp = http.get(devicesUrl(baseUrl), { headers: authHeaders(token) });
  if (resp.status !== 200) {
    throw new Error(`setup: list devices returned ${resp.status}: ${resp.body}`);
  }
  return resp.json() || [];
}

/** What this run created, in the shape the cleanup pass reads. */
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
