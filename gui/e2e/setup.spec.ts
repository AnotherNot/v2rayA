import { test, expect } from "@playwright/test";

// The service starts with an empty configuration directory, so the first
// page is the registration of the admin account. The test leaves the token
// in a storage state file the other project loads.
test("first run registers the admin account", async ({ page }) => {
  // the welcome dialog is not what this test is about
  await page.addInitScript(() => localStorage.setItem("onboardingSeen", "1"));

  await page.goto("/");
  const dialog = page.locator(".v-dialog").filter({
    hasText: "Create an admin account first",
  });
  await expect(dialog).toBeVisible();

  await dialog.locator('input[name="username"]').fill("e2e");
  await dialog.locator('input[name="password"]').fill("e2e-password");
  await dialog.getByRole("button", { name: "Create" }).click();

  await expect(page.locator(".v-dialog")).toHaveCount(0);
  await expect(page.locator(".page__title")).toHaveText("Dashboard");
  await page.context().storageState({ path: "e2e/.state/auth.json" });
});
