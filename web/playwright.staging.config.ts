import { defineConfig } from "@playwright/test";
import local from "./playwright.config";

// Inherits workers, global setup, projects and timeout from the local config; the target, the
// retry policy and the specs that drive the local stack differ.
const shared = { ...local };
const STACK_ONLY = [
  /direct-link\.spec\.ts$/,
  /downloads\.spec\.ts$/,
  /machine-lifecycle\.spec\.ts$/,
  /performance(-chromium)?\.spec\.ts$/,
  /two-routers\.spec\.ts$/,
];
// The deployed staging release needs no local stack.
delete shared.webServer;

export default defineConfig({
  ...shared,
  // Staging has no router, no machine container and no browser server, which these specs drive.
  // A project's own list overrides the config's, so each project carries the exclusions.
  projects: shared.projects
    ?.filter((project) => !project.name?.endsWith("-behind-home-router"))
    .map((project) => ({ ...project, testIgnore: [project.testIgnore ?? [], STACK_ONLY].flat() })),
  // A port-forward can drop a single request; the local stack cannot.
  retries: 1,
  use: {
    ...shared.use,
    baseURL: "http://127.0.0.1:18080",
  },
});
