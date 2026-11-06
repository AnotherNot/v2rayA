import { test, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// The service starts with an empty configuration directory, so the first
// page is the registration of the admin account. The test leaves the token
// in a storage state file the other project loads.
test("first run registers the admin account", async ({ page }) => {
  // the welcome dialog is not what this test is about
  await page.addInitScript(() => localStorage.setItem("onboardingSeen", "1"));

  // start-v2raya.mjs gives the service this configuration directory; the
  // first-run token is written there before the listener accepts requests.
  const setupToken = readFileSync(
    join(process.cwd(), "e2e", ".state", "v2raya", "setup-token"),
    "utf8",
  ).trim();

  await page.goto("/");
  const dialog = page.locator(".v-dialog").filter({
    hasText: "Create an admin account first",
  });
  await expect(dialog).toBeVisible();

  await dialog.locator('input[name="username"]').fill("e2e");
  await dialog.locator('input[name="password"]').fill("e2e-password");
  await dialog.locator('input[name="setup-token"]').fill(setupToken);
  await dialog.getByRole("button", { name: "Create" }).click();

  await expect(page.locator(".v-dialog")).toHaveCount(0);
  await expect(page.locator(".page__title")).toHaveText("Dashboard");
  await page.context().storageState({ path: "e2e/.state/auth.json" });
});
