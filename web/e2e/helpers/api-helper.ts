import type { APIRequestContext } from "@playwright/test";

const BASE = "/api/v1";

export interface AuthToken {
  token: string;
}

export async function register(
  request: APIRequestContext,
  email: string,
  password: string
): Promise<string> {
  const resp = await request.post(`${BASE}/auth/register`, {
    data: { email, password },
  });
  if (!resp.ok()) {
    throw new Error(`register failed: ${resp.status()} ${await resp.text()}`);
  }
  const body: AuthToken = await resp.json();
  return body.token;
}

export async function login(
  request: APIRequestContext,
  email: string,
  password: string
): Promise<string> {
  const resp = await request.post(`${BASE}/auth/login`, {
    data: { email, password },
  });
  if (!resp.ok()) {
    throw new Error(`login failed: ${resp.status()} ${await resp.text()}`);
  }
  const body: AuthToken = await resp.json();
  return body.token;
}

export async function createGroup(
  request: APIRequestContext,
  token: string,
  name: string
): Promise<{ id: string; name: string }> {
  const resp = await request.post(`${BASE}/groups`, {
    data: { name },
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!resp.ok()) {
    throw new Error(
      `createGroup failed: ${resp.status()} ${await resp.text()}`
    );
  }
  return resp.json();
}
