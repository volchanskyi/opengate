// Relay throughput, measured through an actual relay.
//
// This scenario opens the operator's side of a remote session, sends a frame,
// and times the same bytes coming back from the machine. The Go harness holds
// the machine's side and echoes; the server pipes between them. What the metric
// records is therefore the whole relay path.
//
// It has to be that, because three gate ceilings are named after this series,
// and a number filled from anything else leaves those ceilings measuring
// something they were never calibrated against.
import http from "k6/http";
// k6/ws rather than the newer module: ws.connect blocks the iteration until the
// socket closes, which is what lets one iteration send a frame, wait for it, and
// record the round trip as a single measurement. The promise-based module would
// have the iteration end before the answer arrived.
import ws from "k6/ws";
import { check, fail, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";
import {
  authHeaders,
  onlineDeviceIds,
  printCleanupManifest,
  registerMember,
} from "../lib/session.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

const relayMsgLatency = new Trend("relay_msg_latency_ms");
const relayMsgCount = new Counter("relay_msg_count");

// How long the operator's side waits for its own frame to come back before
// giving up on that iteration. Well above any healthy round trip, so a timeout
// is a finding rather than a tight budget.
const ECHO_TIMEOUT_MS = 5000;

export const options = {
  scenarios: {
    relay: {
      executor: "constant-vus",
      vus: 20,
      duration: "1m",
    },
  },
  // The generator runs beside the server, one network hop away, so this is the
  // relay's own round trip: browser side to server, server to machine side, and
  // back. It is deliberately looser than a single HTTP request because the path
  // is three hops and two WebSocket upgrades rather than one request.
  thresholds: {
    relay_msg_latency_ms: ["p(95)<400"],
  },
};

export function setup() {
  const member = registerMember(BASE_URL, "relay");
  const devices = onlineDeviceIds(BASE_URL, member.token);
  if (devices.length === 0) {
    fail(
      "setup: no online machine to open a session against — the QUIC harness must be holding a fleet connected while this scenario runs"
    );
  }
  return { token: member.token, email: member.email, devices };
}

export default function (data) {
  const headers = authHeaders(data.token);

  // One session per iteration, against a machine the harness is holding open.
  const deviceId = data.devices[__ITER % data.devices.length];
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
  // The browser WebSocket API cannot set headers, so the operator's credential
  // travels in the query the way the real client sends it. The relay token says
  // which session; this says who is joining it.
  const relayUrl = `${wsBase(BASE_URL)}/ws/relay/${token}?side=browser&auth=${data.token}`;

  const sentAt = Date.now();
  let echoed = false;

  const res = ws.connect(relayUrl, {}, function (socket) {
    socket.on("open", () => socket.send("opengate-relay-probe"));

    socket.on("message", () => {
      // The frame this operator sent has come back through the machine, so the
      // elapsed time is the whole path rather than one leg of it.
      relayMsgLatency.add(Date.now() - sentAt);
      relayMsgCount.add(1);
      echoed = true;
      socket.close();
    });

    // A machine that never answers is the finding; the socket is closed so the
    // iteration ends rather than holding a relay entry open for the run.
    socket.setTimeout(() => socket.close(), ECHO_TIMEOUT_MS);
  });

  check(res, { "relay upgraded": (r) => r && r.status === 101 });
  check(echoed, { "frame returned from the machine": (ok) => ok === true });

  sleep(1);
}

export function teardown(data) {
  printCleanupManifest([data.email]);
}

// wsBase turns the HTTP origin into the WebSocket one.
function wsBase(baseUrl) {
  return baseUrl.replace(/^http/, "ws");
}
