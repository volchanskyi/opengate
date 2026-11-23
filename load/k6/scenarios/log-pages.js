// Log pages: technicians open a connected machine's log page at the phase's rate, timed from the
// request to the page the machine sent back through the server.
import { check } from "k6";
import { Trend } from "k6/metrics";
// The shared request client sees every request the run makes and counts server refusals.
import {
  authHeaders,
  http,
  ownTechnician,
  printCleanupManifest,
  readFleet,
  techniciansFor,
} from "../lib/session.js";
import { onlineIds } from "../lib/fleet.js";
import {
  arrivalScenarios,
  exportOnly,
  measuredExports,
  phases,
  technicianRoster,
} from "../lib/profile.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// Only a page that was shown is timed, so the count of times is the count of pages shown.
const logPageLatency = new Trend("journey_log_page_ms");

// The lines a technician's first look at a machine's log asks for.
const PAGE_LINES = 100;

// A page may take its whole allowance, so each arrival holds a virtual user for that long.
const PAGE_ALLOWANCE_SECONDS = 30;

const WALK = phases();

export const options = {
  scenarios: arrivalScenarios(WALK, PAGE_ALLOWANCE_SECONDS),
  // The measured phase's pages, completed arrivals and their time, and any dropped arrivals join
  // the export; the profile judges what it names.
  thresholds: Object.assign(
    { dropped_iterations: exportOnly("counter") },
    measuredExports(WALK, {
      iterations: "counter",
      iteration_duration: "trend",
      journey_log_page_ms: "trend",
    })
  ),
};

export function setup() {
  return { technicians: techniciansFor(BASE_URL, technicianRoster("log-pages", WALK)) };
}

export default function (data) {
  const token = ownTechnician(data.technicians).token;
  const headers = authHeaders(token);
  const online = onlineIds(readFleet(BASE_URL, token));
  if (!check(online, { "a machine is connected to read": (ids) => ids.length > 0 })) {
    return;
  }
  const deviceId = online[(__VU - 1) % online.length];
  const page = http.get(`${BASE_URL}/api/v1/devices/${deviceId}/logs?limit=${PAGE_LINES}`, { headers });
  if (check(page, { "log page shown": (r) => r.status === 200 })) {
    logPageLatency.add(page.timings.duration);
  }
}

export function teardown(data) {
  printCleanupManifest(data.technicians.map((technician) => technician.email));
}
