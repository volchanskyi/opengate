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

export interface User {
  id: string;
  email: string;
  display_name: string;
  is_admin: boolean;
}

export interface SecurityGroup {
  id: string;
  name: string;
  description: string;
  is_system: boolean;
}

export interface SecurityGroupWithMembers extends SecurityGroup {
  members: User[];
}

export async function getMe(
  request: APIRequestContext,
  token: string
): Promise<User> {
  const resp = await request.get(`${BASE}/users/me`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!resp.ok()) {
    throw new Error(`getMe failed: ${resp.status()} ${await resp.text()}`);
  }
  return resp.json();
}

export async function listSecurityGroups(
  request: APIRequestContext,
  token: string
): Promise<SecurityGroup[]> {
  const resp = await request.get(`${BASE}/security-groups`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!resp.ok()) {
    throw new Error(
      `listSecurityGroups failed: ${resp.status()} ${await resp.text()}`
    );
  }
  return resp.json();
}

export async function getSecurityGroup(
  request: APIRequestContext,
  token: string,
  id: string
): Promise<SecurityGroupWithMembers> {
  const resp = await request.get(`${BASE}/security-groups/${id}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!resp.ok()) {
    throw new Error(
      `getSecurityGroup failed: ${resp.status()} ${await resp.text()}`
    );
  }
  return resp.json();
}

export async function addGroupMember(
  request: APIRequestContext,
  token: string,
  groupId: string,
  userId: string
): Promise<void> {
  const resp = await request.post(`${BASE}/security-groups/${groupId}/members`, {
    data: { user_id: userId },
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!resp.ok()) {
    throw new Error(
      `addGroupMember failed: ${resp.status()} ${await resp.text()}`
    );
  }
}

export async function removeGroupMember(
  request: APIRequestContext,
  token: string,
  groupId: string,
  userId: string
): Promise<void> {
  const resp = await request.delete(
    `${BASE}/security-groups/${groupId}/members/${userId}`,
    {
      headers: { Authorization: `Bearer ${token}` },
    }
  );
  if (!resp.ok()) {
    throw new Error(
      `removeGroupMember failed: ${resp.status()} ${await resp.text()}`
    );
  }
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
