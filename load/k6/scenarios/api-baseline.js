import http from "k6/http";
import { check, sleep } from "k6";
import { Trend } from "k6/metrics";
import {
  authHeaders,
  devicesUrl,
  printCleanupManifest,
  registerMember,
  visibleSiteIds,
} from "../lib/session.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// One number for the whole interface said nothing useful. Opening a fleet list
// and opening one machine's page are different pieces of work — the second fans
// out to inventory, history and readings — and a technician waits differently
// for each: a list is a glance, a command is a deliberate act. So each is timed
// on its own and each carries the mark that suits it.
const deviceListLatency = new Trend("journey_device_list_ms");
const deviceDetailLatency = new Trend("journey_device_detail_ms");
const commandAcceptLatency = new Trend("journey_command_accept_ms");

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
    // A glance at the fleet.
    "journey_device_list_ms": ["p(95)<300"],
    // One machine's page, which fans out to its inventory, its history and its
    // readings, so it is given more room than the list it was opened from.
    "journey_device_detail_ms": ["p(95)<500"],
    // A deliberate act — putting a machine into maintenance — where the mark is
    // the server accepting the instruction, not the machine carrying it out.
    "journey_command_accept_ms": ["p(95)<1000"],
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
  deviceListLatency.add(devices.timings.duration);

  // One machine's page, and one instruction sent to it. Both need a machine to
  // exist; an empty fleet is a valid shape for this environment, so the two
  // journeys are simply not timed when there is nothing to open.
  const fleet = devices.status === 200 ? devices.json() || [] : [];
  if (fleet.length > 0) {
    const deviceId = fleet[__ITER % fleet.length].id;

    const detail = http.get(`${BASE_URL}/api/v1/devices/${deviceId}`, { headers });
    check(detail, { "device detail 200": (r) => r.status === 200 });
    deviceDetailLatency.add(detail.timings.duration);

    // Maintenance rather than a restart: it is a real instruction a technician
    // sends, it is idempotent, and nothing physical happens at the other end —
    // so the number is the acceptance path without a fleet-wide side effect.
    const command = http.post(
      `${BASE_URL}/api/v1/devices/${deviceId}/maintenance`,
      JSON.stringify({ enabled: false, reason: "load-test acceptance path" }),
      { headers }
    );
    check(command, { "command accepted": (r) => r.status === 200 || r.status === 204 });
    commandAcceptLatency.add(command.timings.duration);
  }

  sleep(1);
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}
