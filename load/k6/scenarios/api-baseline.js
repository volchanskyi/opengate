import http from "k6/http";
import { check, sleep } from "k6";
import {
  authHeaders,
  devicesUrl,
  printCleanupManifest,
  registerMember,
  visibleSiteIds,
} from "../lib/session.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  stages: [
    { duration: "30s", target: 20 },
    { duration: "1m", target: 20 },
    { duration: "30s", target: 0 },
  ],
  // 100 ms rather than 200. The wider figure had cleared every night on the
  // retained trend including the worst one, so it distinguished nothing; the
  // reason it had to be wide was that the generator and the target shared the
  // same two processors, and the measurement's own spread was larger than any
  // regression worth finding. With the two given separate allocations, this is
  // tight enough that a real regression shows.
  thresholds: {
    http_req_duration: ["p(95)<100"],
    http_req_failed: ["rate<0.01"],
  },
};

export function setup() {
  const member = registerMember(BASE_URL, "load");
  return {
    token: member.token,
    email: member.email,
    siteIds: visibleSiteIds(BASE_URL, member.token),
  };
}

export default function (data) {
  const headers = authHeaders(data.token);

  // Health check (no auth)
  const health = http.get(`${BASE_URL}/api/v1/health`);
  check(health, { "health 200": (r) => r.status === 200 });

  // Get current user
  const me = http.get(`${BASE_URL}/api/v1/users/me`, { headers });
  check(me, { "me 200": (r) => r.status === 200 });

  // List sites
  const sites = http.get(`${BASE_URL}/api/v1/sites`, { headers });
  check(sites, { "sites 200": (r) => r.status === 200 });

  // List devices, narrowed to a site when the organization has one
  const devices = http.get(devicesUrl(BASE_URL, data.siteIds[0]), { headers });
  check(devices, { "devices 200": (r) => r.status === 200 });

  sleep(1);
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}
