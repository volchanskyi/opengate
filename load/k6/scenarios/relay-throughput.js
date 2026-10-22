// Relay throughput: times a frame the operator's side sends and the machine's side echoes back.
// k6/ws blocks the iteration until the socket closes, so one iteration records one round trip.
import ws from "k6/ws";
import { check, fail, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";
// The shared request client counts server refusals; the upgrade below is counted by hand.
import {
  authHeaders,
  counted,
  http,
  ownTechnician,
  printCleanupManifest,
  readFleet,
  registerTechnicians,
} from "../lib/session.js";
import { emptyFleetReason, onlineIds } from "../lib/fleet.js";
import {
  measuredExports,
  phases,
  sessionScenarios,
  sessionTechnicians,
} from "../lib/profile.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

const relayMsgLatency = new Trend("relay_msg_latency_ms");
const relayMsgCount = new Counter("relay_msg_count");

// How long the operator's side waits for its frame to come back, well above a healthy round trip.
const ECHO_TIMEOUT_MS = 5000;

// The probe is binary because the browser sends binary agent-protocol frames.
const PROBE = new Uint8Array([
  0x6f, 0x70, 0x65, 0x6e, 0x67, 0x61, 0x74, 0x65, 0x2d, 0x72, 0x65, 0x6c, 0x61,
  0x79, 0x2d, 0x70, 0x72, 0x6f, 0x62, 0x65,
]);

const WALK = phases();

// Sessions held at once: each has a technician and a machine of its own.
const HELD = sessionTechnicians(WALK);

export const options = {
  // The profile's `sessions` sets how many sessions are held open, phase by phase.
  scenarios: sessionScenarios(WALK),
  // Setup registers one account per held session before the walk starts.
  setupTimeout: "10m",
  // The measured phase's keystroke round trip joins the export; the profile judges it.
  thresholds: measuredExports(WALK, { relay_msg_latency_ms: "trend" }),
};

export function setup() {
  const technicians = registerTechnicians(BASE_URL, "relay", HELD);
  const fleet = readFleet(BASE_URL, technicians[0].token);
  const devices = onlineIds(fleet);

  // The read names whether the fleet never arrived (machine side) or the server dropped it.
  const shortfall = emptyFleetReason(fleet);
  if (shortfall) {
    fail(
      `setup: no online machine to open a session against — ${shortfall}. ` +
        "The QUIC harness must be holding a fleet connected while this scenario runs."
    );
  }
  // A machine takes one session at a time, so each held session needs one of its own.
  if (devices.length < HELD) {
    fail(
      `setup: ${devices.length} online machine(s) for ${HELD} sessions held at once; ` +
        "a machine refuses a second technician's session while one is open."
    );
  }
  return { technicians, devices };
}

export default function (data) {
  const technician = ownTechnician(data.technicians);
  const headers = authHeaders(technician.token);

  // One session per iteration, always on this virtual user's own machine.
  const deviceId = data.devices[(__VU - 1) % data.devices.length];
  const created = http.post(
    `${BASE_URL}/api/v1/sessions`,
    JSON.stringify({ device_id: deviceId, permissions: { view_only: true } }),
    { headers }
  );
  if (!check(created, { "session created": (r) => r.status === 201 })) {
    sleep(1);
    return;
  }

  const token = created.json("token");
  // The operator's credential travels in a header, so the address in access logs holds none.
  const relayUrl = `${wsBase(BASE_URL)}/ws/relay/${token}?side=browser`;
  const relayParams = { headers: { Authorization: `Bearer ${technician.token}` } };

  const sentAt = Date.now();
  let echoed = false;

  const res = counted(ws.connect(relayUrl, relayParams, function (socket) {
    socket.on("open", () => socket.sendBinary(PROBE.buffer));

    // The echo has come back through the machine, so the elapsed time is the whole path.
    const recordEcho = () => {
      relayMsgLatency.add(Date.now() - sentAt);
      relayMsgCount.add(1);
      echoed = true;
      socket.close();
    };

    // The relay forwards binary frames and k6 dispatches binary and text to separate handlers,
    // so both are registered and the round trip records whichever type carried it.
    socket.on("binaryMessage", recordEcho);
    socket.on("message", recordEcho);

    // A machine that never answers closes the socket so no relay entry is held for the run.
    socket.setTimeout(() => socket.close(), ECHO_TIMEOUT_MS);
  }));

  check(res, { "relay upgraded": (r) => r && r.status === 101 });
  check(echoed, { "frame returned from the machine": (ok) => ok === true });

  sleep(1);
}

export function teardown(data) {
  printCleanupManifest(data.technicians.map((technician) => technician.email));
}

// wsBase turns the HTTP origin into the WebSocket one.
function wsBase(baseUrl) {
  return baseUrl.replace(/^http/, "ws");
}
