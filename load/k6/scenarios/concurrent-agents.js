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
    { duration: "30s", target: 30 },
    { duration: "1m", target: 30 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(99)<500"],
    http_req_failed: ["rate<0.001"],
  },
};

export function setup() {
  // One shared member for every VU, reading the sites the organization has.
  const member = registerMember(BASE_URL, "agent-load");
  return {
    token: member.token,
    email: member.email,
    siteIds: visibleSiteIds(BASE_URL, member.token),
  };
}

export default function (data) {
  const headers = authHeaders(data.token);

  // Simulate agent-like HTTP operations at scale
  const health = http.get(`${BASE_URL}/api/v1/health`);
  check(health, { "health ok": (r) => r.status === 200 });

  // Spread the device reads across the sites in the fleet
  const idx = Math.floor(Math.random() * data.siteIds.length);
  const devices = http.get(devicesUrl(BASE_URL, data.siteIds[idx]), { headers });
  check(devices, { "devices ok": (r) => r.status === 200 });

  // List sessions (even if empty)
  const sessions = http.get(
    `${BASE_URL}/api/v1/sessions?device_id=00000000-0000-0000-0000-000000000000`,
    { headers }
  );
  check(sessions, { "sessions ok": (r) => r.status === 200 });

  sleep(1);
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}
