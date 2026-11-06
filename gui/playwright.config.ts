import { defineConfig, devices } from "@playwright/test";

// The browser tests run against the service itself: its embedded interface
// is what a user gets, and the API is real. `./build-all.sh` must have run
// from the repository root; the webServer script starts the binary with a
// fresh configuration directory.
const port = Number(process.env.V2RAYA_E2E_PORT ?? 21117);
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  // One service, one account: the tests share the state the setup project
  // leaves behind, so they run in order.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [["github"], ["html", { open: "never" }]]
    : [["list"], ["html", { open: "never" }]],
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "setup",
      testMatch: /setup\.spec\.ts/,
    },
    {
      name: "chromium",
      testIgnore: /setup\.spec\.ts/,
      dependencies: ["setup"],
      use: {
        ...devices["Desktop Chrome"],
        storageState: "e2e/.state/auth.json",
      },
    },
  ],
  webServer: {
    command: "node e2e/start-v2raya.mjs",
    url: `${baseURL}/api/version`,
    env: { V2RAYA_E2E_PORT: String(port) },
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
