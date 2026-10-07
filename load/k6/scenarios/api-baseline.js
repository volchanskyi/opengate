import { check } from "k6";
import { Trend } from "k6/metrics";
// The shared request client sees every request the run makes and counts server refusals.
import {
  anonymousHeaders,
  authHeaders,
  devicesUrl,
  http,
  printCleanupManifest,
  registerMember,
  siteWithDevices,
} from "../lib/session.js";
import {
  arrivalScenarios,
  measuredThresholds,
  phases,
} from "../lib/profile.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// Each journey is timed on its own: a device page fans out to inventory, history and readings.
const deviceListLatency = new Trend("journey_device_list_ms");
const deviceDetailLatency = new Trend("journey_device_detail_ms");
const commandAcceptLatency = new Trend("journey_command_accept_ms");

// Requests one journey makes; it sets how long a virtual user is held and so how many a rate needs.
const REQUESTS_PER_JOURNEY = 6;

const WALK = phases();

export const options = {
  scenarios: arrivalScenarios(WALK, REQUESTS_PER_JOURNEY),
  // Run-wide marks, repeated over the measured phase; naming its sub-metric puts it in the export.
  thresholds: Object.assign(
    {
      http_req_duration: ["p(95)<100"],
      http_req_failed: ["rate<0.01"],
      // A glance at the fleet.
      "journey_device_list_ms": ["p(95)<300"],
      // One machine's page fans out to inventory, history and readings, so it gets more room.
      "journey_device_detail_ms": ["p(95)<500"],
      // The mark is the server accepting the maintenance instruction, not the machine acting on it.
      "journey_command_accept_ms": ["p(95)<1000"],
      // The generator could not keep the rate the profile declared.
      dropped_iterations: ["count<1"],
    },
    measuredThresholds(WALK, {
      http_req_duration: ["p(95)<100"],
      http_req_failed: ["rate<0.01"],
    })
  ),
};

export function setup() {
  const member = registerMember(BASE_URL, "load");
  return {
    token: member.token,
    email: member.email,
    // The site holds machines, because two journeys are timed against one.
    siteId: siteWithDevices(BASE_URL, member.token),
  };
}

export default function (data) {
  const headers = authHeaders(data.token);
  const anonymous = anonymousHeaders();

  // Health check (no auth), still from this technician's address: the allowance
  // is spent per address whether the request was signed in or not.
  const health = http.get(`${BASE_URL}/api/v1/health`, { headers: anonymous });
  check(health, { "health 200": (r) => r.status === 200 });

  // Get current user
  const me = http.get(`${BASE_URL}/api/v1/users/me`, { headers });
  check(me, { "me 200": (r) => r.status === 200 });

  // List sites
  const sites = http.get(`${BASE_URL}/api/v1/sites`, { headers });
  check(sites, { "sites 200": (r) => r.status === 200 });

  // List devices, narrowed to a site when the organization has one
  const devices = http.get(devicesUrl(BASE_URL, data.siteId), { headers });
  check(devices, { "devices 200": (r) => r.status === 200 });
  deviceListLatency.add(devices.timings.duration);

  // The device page and command journeys need a machine and are skipped on an empty fleet.
  const fleet = devices.status === 200 ? devices.json() || [] : [];
  if (fleet.length > 0) {
    const deviceId = fleet[__ITER % fleet.length].id;

    const detail = http.get(`${BASE_URL}/api/v1/devices/${deviceId}`, { headers });
    check(detail, { "device detail 200": (r) => r.status === 200 });
    deviceDetailLatency.add(detail.timings.duration);

    // Maintenance is an idempotent instruction with no physical effect, so it times acceptance alone.
    const command = http.post(
      `${BASE_URL}/api/v1/devices/${deviceId}/maintenance`,
      JSON.stringify({ enabled: false, reason: "load-test acceptance path" }),
      { headers }
    );
    check(command, { "command accepted": (r) => r.status === 200 || r.status === 204 });
    commandAcceptLatency.add(command.timings.duration);
  }
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}
