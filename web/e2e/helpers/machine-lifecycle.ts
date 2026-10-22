/// <reference types="node" />
import { execFileSync, spawnSync } from "node:child_process";
import type { APIRequestContext } from "@playwright/test";
import { adminToken } from "./enrolled-machine";

// The test stack's third machine and its copy, driven as a technician's installs and power cuts
// would drive a real one: through the container and its data folder, never inside the agent.

export const MACHINE_C = "agent-c";
export const MACHINE_C_COPY = "agent-c-copy";

/** Where the agent keeps its identity, its note and its removal marker. */
const DATA_DIR = "/var/lib/mesh-agent";

/** The address the stack's machines reach the server by, as the installer writes it in a note. */
const NOTE_URL = "http://server:8080";

const container = (machine: string) => `opengate-test-${machine}`;

function docker(...args: string[]): string {
  return execFileSync("docker", args, { encoding: "utf8" }).trim();
}

export function stopMachine(machine: string): void {
  docker("stop", "-t", "10", container(machine));
}

/** Cuts the machine's power: the agent gets no chance to finish what it was doing. */
export function cutPower(machine: string): void {
  docker("kill", container(machine));
}

export function startMachine(machine: string): void {
  docker("start", container(machine));
}

/** Whether the machine's agent is running, and the code it last exited with. */
export function machineState(machine: string): { running: boolean; exitCode: number } {
  const [running, exitCode] = docker("inspect", "-f", "{{.State.Running}} {{.State.ExitCode}}", container(machine)).split(" ");
  return { running: running === "true", exitCode: Number(exitCode) };
}

/** Everything the machine's agent has logged since its container was created. */
export function machineLog(machine: string): string {
  const out = spawnSync("docker", ["logs", container(machine)], { encoding: "utf8" });
  return `${out.stdout}${out.stderr}`;
}

function volumeOf(machine: string): string {
  return docker("inspect", "-f", `{{range .Mounts}}{{if eq .Destination "${DATA_DIR}"}}{{.Name}}{{end}}{{end}}`, container(machine));
}

/** Runs a script as root over the machine's data folder at /data, running or stopped. */
function overDataFolder(machine: string, script: string, input?: string): string {
  const image = docker("inspect", "-f", "{{.Config.Image}}", container(machine));
  return execFileSync(
    "docker",
    ["run", "--rm", "-i", "-u", "0", "--entrypoint", "sh", "-v", `${volumeOf(machine)}:/data`, image, "-c", script],
    { encoding: "utf8", input },
  ).trim();
}

/** The names in the machine's data folder. */
export function dataFolder(machine: string): string[] {
  const listed = overDataFolder(machine, "ls -A /data");
  return listed === "" ? [] : listed.split("\n");
}

/** Empties the machine's data folder, as a machine that was never installed holds. */
export function wipeDataFolder(machine: string): void {
  overDataFolder(machine, "find /data -mindepth 1 -delete");
}

/** Writes a data-folder file as the installer does: the agent's user owns it, and only it. */
export function placeFile(machine: string, name: string, content: string): void {
  overDataFolder(
    machine,
    `owner="$(stat -c %u:%g /data)" && cat >"/data/${name}" && chown "$owner" "/data/${name}" && chmod 0600 "/data/${name}"`,
    content,
  );
}

/** Copies one machine's data folder over another's, as cloning a disk would. */
export function copyDataFolder(from: string, to: string): void {
  const image = docker("inspect", "-f", "{{.Config.Image}}", container(from));
  docker(
    "run", "--rm", "-u", "0", "--entrypoint", "sh",
    "-v", `${volumeOf(from)}:/from:ro`, "-v", `${volumeOf(to)}:/to`, image,
    "-c", "find /to -mindepth 1 -delete && cp -a /from/. /to/",
  );
}

const asAdmin = () => ({ headers: { Authorization: `Bearer ${adminToken()}` } });

export interface Device {
  id: string;
  hostname: string;
  status: string;
  created_at: string;
  identity_shared_at?: string;
}

export async function devices(request: APIRequestContext): Promise<Device[]> {
  const resp = await request.get("/api/v1/devices", asAdmin());
  if (!resp.ok()) throw new Error(`listing devices: ${resp.status()} ${await resp.text()}`);
  return resp.json();
}

/** The device with this number, or null once it is deleted. */
export async function deviceById(request: APIRequestContext, id: string): Promise<Device | null> {
  const resp = await request.get(`/api/v1/devices/${id}`, asAdmin());
  if (resp.status() === 404) return null;
  if (!resp.ok()) throw new Error(`reading device ${id}: ${resp.status()} ${await resp.text()}`);
  return resp.json();
}

export async function deleteDevice(request: APIRequestContext, id: string): Promise<void> {
  const resp = await request.delete(`/api/v1/devices/${id}`, asAdmin());
  if (!resp.ok() && resp.status() !== 404) throw new Error(`deleting device ${id}: ${resp.status()} ${await resp.text()}`);
}

/** A join code of its own, so a case can read how often it was spent. */
export async function mintJoinCode(request: APIRequestContext, label: string): Promise<{ id: string; token: string }> {
  const resp = await request.post("/api/v1/enrollment-tokens", {
    ...asAdmin(),
    data: { label, max_uses: 0, expires_in_hours: 1 },
  });
  if (!resp.ok()) throw new Error(`minting a join code: ${resp.status()} ${await resp.text()}`);
  return resp.json();
}

export async function joinCodeUses(request: APIRequestContext, id: string): Promise<number> {
  const resp = await request.get("/api/v1/enrollment-tokens", asAdmin());
  const codes: { id: string; use_count: number }[] = await resp.json();
  const code = codes.find((c) => c.id === id);
  if (!code) throw new Error(`join code ${id} is not listed`);
  return code.use_count;
}

/** The note the installer leaves for a join code, with the master key the probe announced. */
export async function installNote(request: APIRequestContext, token: string, fresh: boolean): Promise<string> {
  const probe = await request.post(`/api/v1/enroll/${token}`, { data: { csr_pem: "" } });
  const { ca_pem } = (await probe.json()) as { ca_pem?: string };
  if (!ca_pem) throw new Error(`the enrolment probe returned no master key: ${probe.status()}`);
  return JSON.stringify({ token, url: NOTE_URL, ca_pem, fresh });
}
