// Session helpers shared by the k6 scenarios.
//
// The scenarios drive the API as an ordinary organization member, which is what
// a load generator can be: organization is the visibility boundary, so a member
// reads the whole fleet, while creating or deleting a site is administrator
// work the server refuses. A scenario that stands up its own fixtures would get
// a 403 and then measure the error path, so the scenarios read the fleet the
// staging organization already has.
import http from "k6/http";

/** Authorization headers for a bearer token. */
export function authHeaders(token) {
  return {
    "Content-Type": "application/json",
    Authorization: `Bearer ${token}`,
  };
}

/**
 * Register a throwaway member of the staging organization and return its token.
 * Throws on any unexpected status: a load run against an unusable session
 * measures nothing, and failing here names the real cause instead of leaving
 * every request in the run to fail for a reason the summary cannot show.
 */
export function registerMember(baseUrl, prefix) {
  const email = `${prefix}-${Date.now()}-${__VU}@test.local`;
  const resp = http.post(
    `${baseUrl}/api/v1/auth/register`,
    JSON.stringify({ email, password: "LoadTestPass123!" }),
    { headers: { "Content-Type": "application/json" } }
  );
  if (resp.status !== 201) {
    throw new Error(`setup: register returned ${resp.status}: ${resp.body}`);
  }
  const token = resp.json("token");
  if (!token) {
    throw new Error("setup: register returned no token");
  }
  return token;
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
