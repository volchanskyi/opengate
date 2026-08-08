import http from "k6/http";
import { check, sleep } from "k6";
import { authHeaders, registerMember, visibleSiteIds, devicesUrl } from "../lib/session.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  stages: [
    { duration: "30s", target: 20 },
    { duration: "1m", target: 20 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(95)<200"],
    http_req_failed: ["rate<0.01"],
  },
};

export function setup() {
  const token = registerMember(BASE_URL, "load");
  return { token, siteIds: visibleSiteIds(BASE_URL, token) };
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
