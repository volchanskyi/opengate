import http from "k6/http";
import { check } from "k6";
import { Trend } from "k6/metrics";
import {
  anonymousHeaders,
  authHeaders,
  devicesUrl,
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

// One number for the whole interface said nothing useful. Opening a fleet list
// and opening one machine's page are different pieces of work — the second fans
// out to inventory, history and readings — and a technician waits differently
// for each: a list is a glance, a command is a deliberate act. So each is timed
// on its own and each carries the mark that suits it.
const deviceListLatency = new Trend("journey_device_list_ms");
const deviceDetailLatency = new Trend("journey_device_detail_ms");
const commandAcceptLatency = new Trend("journey_command_accept_ms");

// Requests one journey makes. It is what decides how long a virtual user is
// held, and therefore how many of them a declared arrival rate needs.
const REQUESTS_PER_JOURNEY = 6;

const WALK = phases();

export const options = {
  scenarios: arrivalScenarios(WALK, REQUESTS_PER_JOURNEY),
  // The run-wide marks, and the same marks over the phase the profile says the
  // night's numbers are taken from. Naming a phase's sub-metric here is also
  // what puts it in the summary export, which is how the stored row comes to
  // hold the load's own figures rather than a mixture of the load, the climb to
  // it and the wind-down away from it.
  //
  // 100 ms rather than 200. The wider figure had cleared every night on the
  // retained trend including the worst one, so it distinguished nothing; the
  // reason it had to be wide was that the generator and the target shared the
  // same two processors, and the measurement's own spread was larger than any
  // regression worth finding. With the two given separate allocations, this is
  // tight enough that a real regression shows.
  thresholds: Object.assign(
    {
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
      // The generator saying it could not keep the rate the profile declared.
      // Without it, an arrival-rate run degrades quietly back into the closed
      // loop it replaced: offered load falls, latency stays flat, and the night
      // reports a healthy server it never finished asking.
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
    // The site the fleet is read from is chosen for holding machines. Two of the
    // journeys below are timed against one, so a site picked for sorting first
    // leaves them recording nothing on every night it happens to be empty.
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
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}
