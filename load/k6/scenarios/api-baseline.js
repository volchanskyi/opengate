import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  stages: [
    { duration: "30s", target: 50 },
    { duration: "1m", target: 50 },
    { duration: "30s", target: 0 },
  ],
  thresholds: {
    http_req_duration: ["p(95)<200"],
    http_req_failed: ["rate<0.01"],
  },
};

export function setup() {
  const email = `load-${Date.now()}@test.local`;
  const regResp = http.post(
    `${BASE_URL}/api/v1/auth/register`,
    JSON.stringify({ email, password: "LoadTestPass123!" }),
    { headers: { "Content-Type": "application/json" } }
  );
  const token = regResp.json("token");

  const groupResp = http.post(
    `${BASE_URL}/api/v1/groups`,
    JSON.stringify({ name: `load-group-${Date.now()}` }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
    }
  );
  const groupId = groupResp.json("id");

  return { token, groupId };
}

export default function (data) {
  const headers = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${data.token}`,
  };

  // Health check (no auth)
  const health = http.get(`${BASE_URL}/api/v1/health`);
  check(health, { "health 200": (r) => r.status === 200 });

  // Get current user
  const me = http.get(`${BASE_URL}/api/v1/users/me`, { headers });
  check(me, { "me 200": (r) => r.status === 200 });

  // List groups
  const groups = http.get(`${BASE_URL}/api/v1/groups`, { headers });
  check(groups, { "groups 200": (r) => r.status === 200 });

  // List devices in group
  const devices = http.get(
    `${BASE_URL}/api/v1/devices?group_id=${data.groupId}`,
    { headers }
  );
  check(devices, { "devices 200": (r) => r.status === 200 });

  sleep(0.5);
}
