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
import client from "k6/http";
import { Counter } from "k6/metrics";

/**
 * The status the server answers a request it will not serve with. Read against
 * the limiter that writes it by scripts/tests/loadtest-rate-budget.test.sh, so
 * a count of the wrong number cannot sit here reporting clean nights.
 */
const REFUSED_STATUS = 429;

/**
 * How many of this run's requests the server turned away at the door.
 *
 * The server counts requests per address, and a night whose presented addresses
 * were not believed spends one allowance between every virtual user — so it
 * fills with refusals, reds the error-rate gate, and looks exactly like a night
 * against a slow server. One is a broken test setup and one is a finding about
 * the product, and nothing separated them: what k6 publishes about failures is
 * a single pass/fail rate with no breakdown by status.
 *
 * A nought is added on every answered request, not only on a refusal. A k6
 * counter nobody increments is left out of the export entirely, so a series that
 * appeared only once something was refused would mean "nobody was refused" and
 * "this scenario never counted" at once — and scripts/loadtest-bundle-merge.sh
 * refuses an export that made requests and carries no count of them.
 */
const refused = new Counter("requests_refused");

/**
 * Count one answer, and hand it back to the caller unchanged.
 *
 * Exported for the one answer that does not come back through the client below:
 * a WebSocket upgrade, which k6 opens through a module of its own. The allowance
 * is spent per address whatever the request was for, so an upgrade refused at
 * the door belongs in the same count.
 */
export function counted(response) {
  refused.add(response && response.status === REFUSED_STATUS ? 1 : 0);
  return response;
}

/**
 * The request client every scenario uses, in place of k6's own.
 *
 * It is the one place that sees every request a run makes — the journeys, the
 * sessions, and the setup reads below — which is what makes the count above a
 * count of the whole run rather than of whichever call sites somebody
 * remembered. A scenario reaching for `k6/http` itself is refused by
 * scripts/tests/loadtest-rate-budget.test.sh.
 *
 * The verbs are the ones the scenarios use. A scenario needing another adds it
 * here, which is the point: there is no second way to make a request.
 */
export const http = {
  get: (url, params) => counted(client.get(url, params)),
  post: (url, body, params) => counted(client.post(url, body, params)),
};

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

/**
 * The address one simulated technician presents.
 *
 * The server counts requests per address, so a scenario whose virtual users all
 * leave one pod spends one allowance between them however many technicians it
 * is pretending to be — and what it then measures is the allowance rather than
 * the server. Real technicians arrive from many addresses; presenting one each
 * is the faithful shape as well as the one that lets the load through.
 *
 * The range is 198.18.0.0/15, which exists for exactly this (RFC 2544 reserves
 * it for benchmarking) and belongs to nobody, so a synthetic address can never
 * be a real one. It holds 131,072 addresses, which is more virtual users than
 * any generator here can hold open.
 *
 * The server believes a presented address only from a peer it has been told is
 * a proxy, and a load generator is named as one in the environment it runs in
 * and nowhere else — so this claims nothing on a deployment that has not said
 * so.
 */
export function presentedAddress(index) {
  const offset = Math.abs(index | 0) % 131072;
  return `198.${18 + (offset >> 16)}.${(offset >> 8) & 255}.${offset & 255}`;
}

/**
 * How many addresses one scenario's block holds, and how many blocks there are.
 * The product is the whole range, so the blocks tile it exactly.
 */
const ADDRESSES_PER_SCENARIO = 8192;
const SCENARIO_BLOCKS = 16;

/**
 * The block of addresses this scenario presents from.
 *
 * The browser-side scenarios run at the same time against the same server, and
 * an address is an allowance — so two of them presenting the same address would
 * share one between them and measure the allowance rather than the server.
 * Derived from the scenario's own name rather than handed in, so a scenario
 * cannot be added without a block and no table has to be kept level with the
 * files beside it. That the repository's own names land in different blocks is
 * checked by scripts/tests/loadtest-rate-budget.test.sh.
 */
export function scenarioBlock(name) {
  let hash = 2166136261;
  for (let i = 0; i < name.length; i++) {
    hash ^= name.charCodeAt(i);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash % SCENARIO_BLOCKS;
}

/**
 * The address this virtual user presents. One per virtual user, stable for the
 * life of the run, so a technician spends its own allowance from its first
 * request to its last.
 */
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

/**
 * Headers for a request that carries no session — a health check. It still
 * presents the technician's address, because the allowance is spent per address
 * whether the request was signed in or not.
 */
export function anonymousHeaders() {
  return { "X-Forwarded-For": technicianAddress() };
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
 * The first site that actually holds machines, or undefined when none does.
 *
 * A journey timed against a machine needs the fleet read to return one, and a
 * site is not guaranteed to hold any: an estate spreads its machines over its
 * sites, and asking whichever site sorts first is asking an arbitrary one of
 * them. When it holds none the read is legitimately empty, the journeys are
 * skipped, and their trends publish a zero indistinguishable from a fast night.
 *
 * Undefined is the honest answer when no site has machines, and `devicesUrl`
 * already reads it as the unfiltered fleet request.
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

/**
 * Device-list URL, narrowed to `siteId` when one is given. An absent site id
 * means no site holds machines, so the unfiltered fleet read is the request a
 * client would actually make.
 */
export function devicesUrl(baseUrl, siteId) {
  return siteId
    ? `${baseUrl}/api/v1/devices?site_id=${siteId}`
    : `${baseUrl}/api/v1/devices`;
}

/**
 * The whole fleet as the server answered with it, rather than the part of it a
 * scenario wants.
 *
 * A session can only be opened against a machine that is connected, so the
 * scenario that needs one keeps the machines that answer — but which machines
 * were *there* is the half that says why an empty answer is empty, and a reader
 * that filtered here threw it away before anybody could look. fleet.js decides
 * on what this returns.
 */
export function readFleet(baseUrl, token) {
  const resp = http.get(devicesUrl(baseUrl), { headers: authHeaders(token) });
  if (resp.status !== 200) {
    throw new Error(`setup: list devices returned ${resp.status}: ${resp.body}`);
  }
  return resp.json() || [];
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
