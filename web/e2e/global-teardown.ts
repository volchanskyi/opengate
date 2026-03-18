import { execSync } from "child_process";
import { fileURLToPath } from "url";
import { resolve, dirname } from "path";

export default async function globalTeardown() {
  if (process.env.CI) return;

  // Only tear down if the test container is actually running.
  try {
    const out = execSync("docker inspect -f '{{.State.Running}}' opengate-test-server 2>/dev/null", {
      encoding: "utf-8",
    }).trim();
    if (out !== "true") return;
  } catch {
    return; // Container doesn't exist — nothing to tear down.
  }

  const dir = dirname(fileURLToPath(import.meta.url));
  const deployDir = resolve(dir, "../../deploy");

  execSync("docker compose -f docker-compose.test.yml down -v", {
    cwd: deployDir,
    stdio: "inherit",
  });
}
