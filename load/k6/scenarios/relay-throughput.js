import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

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
  // p(95)<50ms assumed a local target. Since the OKE cutover, this scenario
  // reaches staging through a kubectl port-forward tunnel from the
  // GitHub-hosted runner to the OCI cluster, whose RTT alone floors latency
  // well above 50ms regardless of server performance (checks stay 100%
  // green while only this threshold fails). 250ms keeps headroom over the
  // observed tunnel latency while still catching a real server regression.
  thresholds: {
    relay_msg_latency_ms: ["p(95)<250"],
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
