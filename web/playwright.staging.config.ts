import { defineConfig } from "@playwright/test";
import local from "./playwright.config";

// Inherits workers, global setup, projects and timeout from the local config; only the target
// and the retry policy differ.
const shared = { ...local };
// The deployed staging release needs no local stack.
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
