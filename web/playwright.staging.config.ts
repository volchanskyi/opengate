import { defineConfig } from "@playwright/test";
import local from "./playwright.config";

// The staging run executes the same specs as the local/CI run, against the
// deployed staging release reached through a kubectl port-forward. Everything
// that governs HOW the suite executes — worker count, global setup, project
// list, per-test timeout — is inherited from the local config, so a local pass
// predicts a staging pass. Only the target and the retry policy differ.
//
// `workers: 1` in particular is load-bearing and inherited on purpose: the
// admin-promotion fixtures share server-side IAM state, and the organization is
// the visibility boundary for groups and devices, so parallel workers observe
// each other's fleet writes.
//
// Parity is pinned by scripts/tests/playwright-config-parity.test.sh.
const shared = { ...local };
// Staging is already deployed; there is no local stack to bring up.
delete shared.webServer;

export default defineConfig({
  ...shared,
  // A port-forward can drop a single request; the local stack cannot.
  retries: 1,
  use: {
    ...shared.use,
    baseURL: "http://127.0.0.1:18080",
  },
});
