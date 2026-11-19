// Signs in, before the walk, the technicians one scenario's run takes, and writes their accounts
// to LOADTEST_TECHNICIANS_OUT, where that run reads them.
import { phases, technicianRoster } from "./lib/profile.js";
import { registerTechnicians } from "./lib/session.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

const ROSTER = technicianRoster(__ENV.LOADTEST_SCENARIO, phases());

const OUT = __ENV.LOADTEST_TECHNICIANS_OUT;
if (!OUT) {
  throw new Error("LOADTEST_TECHNICIANS_OUT is unset: the accounts would be signed in for nobody");
}

export const options = {
  // Each sign-up has the server hash a password, which takes its time on a small allowance.
  setupTimeout: "10m",
  scenarios: { "sign-in": { executor: "shared-iterations", vus: 1, iterations: 1 } },
};

export function setup() {
  return registerTechnicians(BASE_URL, ROSTER.prefix, ROSTER.count);
}

export default function () {}

/** The accounts setup signed in, as the one file this run leaves. */
export function handleSummary(data) {
  return { [OUT]: JSON.stringify(data.setup_data) };
}
