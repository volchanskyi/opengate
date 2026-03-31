import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  stages: [
    { duration: "30s", target: 30 },
    { duration: "1m", target: 30 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(99)<500"],
    http_req_failed: ["rate<0.001"],
  },
};

export function setup() {
  // Register a shared user for all VUs
  const email = `agent-load-${Date.now()}@test.local`;
  const regResp = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, password: "AgentLoadPass123!" }),
    { headers: { "Content-Type": "application/json" } }
  );
  const token = regResp.json("token");

  // Create multiple groups to distribute load
  const groupIds = [];
  for (let i = 0; i < 10; i++) {
    const resp = http.post(
      `${BASE_URL}/api/v1/groups`,
      JSON.stringify({ name: `load-group-${Date.now()}-${i}` }),
      {
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
      }
    );
    groupIds.push(resp.json("id"));
  }

  return { token, groupIds };
}

export default function (data) {
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${data.token}`,
  };

  // Simulate agent-like HTTP operations at scale
  const health = http.get(`${BASE_URL}/api/v1/health`);
  check(health, { "health ok": (r) => r.status === 200 });

  // Random group device listing
  const idx = Math.floor(Math.random() * data.groupIds.length);
  const devices = http.get(
    `${BASE_URL}/api/v1/devices?group_id=${data.groupIds[idx]}`,
    { headers }
  );
  check(devices, { "devices ok": (r) => r.status === 200 });

  // List sessions (even if empty)
  const sessions = http.get(
    `${BASE_URL}/api/v1/sessions?device_id=00000000-0000-0000-0000-000000000000`,
    { headers }
  );
  check(sessions, { "sessions ok": (r) => r.status === 200 });

  sleep(1);
}
