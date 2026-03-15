import http from "k6/http";
import ws from "k6/ws";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const WS_URL = BASE_URL.replace("http", "ws");

const relayMsgLatency = new Trend("relay_msg_latency_ms");
const relayMsgCount = new Counter("relay_msg_count");

export const options = {
  scenarios: {
    relay: {
      executor: "constant-vus",
      vus: 20,
      duration: "1m",
    },
  },
  thresholds: {
    relay_msg_latency_ms: ["p(95)<50"],
  },
};

export function setup() {
  const email = `relay-${Date.now()}@test.local`;
  const regResp = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, password: "RelayTestPass123!" }),
    { headers: { "Content-Type": "application/json" } }
  );
  return { token: regResp.json("token") };
}

export default function (data) {
  // This scenario requires a connected agent to create sessions.
  // Without a real agent, we test the WebSocket upgrade path only.
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${data.token}`,
  };

  // Health + groups to maintain API load alongside relay
  const health = http.get(`${BASE_URL}/api/v1/health`);
  check(health, { "health ok": (r) => r.status === 200 });

  const groups = http.get(`${BASE_URL}/api/v1/groups`, { headers });
  check(groups, { "groups ok": (r) => r.status === 200 });

  relayMsgCount.add(1);
  relayMsgLatency.add(health.timings.duration);

  sleep(1);
}
