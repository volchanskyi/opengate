import { check } from "k6";
import { Trend } from "k6/metrics";
// The shared request client sees every request the run makes and counts server refusals.
import {
  anonymousHeaders,
  authHeaders,
  devicesUrl,
  http,
  ownTechnician,
  printCleanupManifest,
  registerTechnicians,
  siteWithDevices,
} from "../lib/session.js";
import {
  arrivalScenarios,
  arrivalTechnicians,
  exportOnly,
  measuredExports,
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
  // The measured phase's requests, journeys, completed arrivals and their time, and any dropped
  // arrivals join the export; the profile judges what it names.
  thresholds: Object.assign(
    { dropped_iterations: exportOnly("counter") },
    measuredExports(WALK, {
      http_req_duration: "trend",
      http_req_failed: "rate",
      iterations: "counter",
      iteration_duration: "trend",
      journey_device_list_ms: "trend",
      journey_device_detail_ms: "trend",
      journey_command_accept_ms: "trend",
    })
  ),
};

export function setup() {
  const technicians = registerTechnicians(
    BASE_URL,
    "load",
    arrivalTechnicians(WALK, REQUESTS_PER_JOURNEY)
  );
  // The site holds machines, because two journeys are timed against one of them.
  const { siteId, deviceIds } = siteWithDevices(BASE_URL, technicians[0].token);
  return { technicians, siteId, deviceIds };
}

export default function (data) {
  const headers = authHeaders(ownTechnician(data.technicians).token);
  const anonymous = anonymousHeaders();

  // Health check (no auth), from this technician's address: a request carrying no account is
  // counted under its address.
  const health = http.get(`${BASE_URL}/api/v1/health`, { headers: anonymous });
  check(health, { "health 200": (r) => r.status === 200 });

  // Get current user
  const me = http.get(`${BASE_URL}/api/v1/users/me`, { headers });
  check(me, { "me 200": (r) => r.status === 200 });

  // List sites
  const sites = http.get(`${BASE_URL}/api/v1/sites`, { headers });
  check(sites, { "sites 200": (r) => r.status === 200 });

  // The whole list is asked for and timed as the console asks for it; its body grows with the fleet,
  // so none of it is kept.
  const devices = http.get(devicesUrl(BASE_URL, data.siteId), { headers, responseType: "none" });
  check(devices, { "devices 200": (r) => r.status === 200 });
  deviceListLatency.add(devices.timings.duration);

  // The device page and command journeys need a machine and are skipped on an empty fleet.
  if (data.deviceIds.length > 0) {
    const deviceId = data.deviceIds[__ITER % data.deviceIds.length];

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
  printCleanupManifest(data.technicians.map((technician) => technician.email));
}
