import { check } from "k6";
// The shared request client sees every request the run makes and counts server refusals.
import {
  anonymousHeaders,
  authHeaders,
  devicesUrl,
  http,
  ownTechnician,
  printCleanupManifest,
  registerTechnicians,
  visibleSiteIds,
} from "../lib/session.js";
import {
  arrivalScenarios,
  arrivalTechnicians,
  exportOnly,
  measuredExports,
  phases,
} from "../lib/profile.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

// Requests one pass makes.
const REQUESTS_PER_PASS = 3;

const WALK = phases();

export const options = {
  // Arrivals keep the declared rate when the server slows; dropped_iterations reports a shortfall.
  scenarios: arrivalScenarios(WALK, REQUESTS_PER_PASS),
  // The measured phase's requests and any dropped arrivals join the export; the profile judges them.
  thresholds: Object.assign(
    { dropped_iterations: exportOnly("counter") },
    measuredExports(WALK, { http_req_duration: "trend", http_req_failed: "rate" })
  ),
};

export function setup() {
  const technicians = registerTechnicians(
    BASE_URL,
    "agent-load",
    arrivalTechnicians(WALK, REQUESTS_PER_PASS)
  );
  return {
    technicians,
    siteIds: visibleSiteIds(BASE_URL, technicians[0].token),
  };
}

export default function (data) {
  const headers = authHeaders(ownTechnician(data.technicians).token);

  // Simulate agent-like HTTP operations at scale
  const health = http.get(`${BASE_URL}/api/v1/health`, { headers: anonymousHeaders() });
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
}

export function teardown(data) {
  printCleanupManifest(data.technicians.map((technician) => technician.email));
}
