import { test, expect, type Page } from "@playwright/test";

// A REALITY link carrying both fields this fork and upstream added: the
// REALITY ML-DSA-65 verify key (pqv) and the VLESS encryption parameter.
// The address is deliberately unreachable; the tests read what the
// interface keeps, not whether the tunnel connects.
const nodeLink =
  "vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:1" +
  "?type=tcp&security=reality&sni=example.com&pbk=public-key&sid=0123" +
  "&fp=chrome&pqv=verify-key&encryption=mlkem.testonly#e2e-node";

async function openDestination(page: Page, label: string) {
  await page.locator(".drawer__item").filter({ hasText: label }).click();
  await expect(page.locator(".page__title")).toHaveText(label);
}

/** The menu button of the node card named `name`. */
function nodeCard(page: Page, name: string) {
  return page.locator(".node-card").filter({ hasText: name });
}

test("imports a VLESS link and keeps encryption and pqv", async ({ page }) => {
  await page.goto("/");
  await openDestination(page, "Proxies");

  await page.getByRole("button", { name: "Import" }).first().click();
  const importDialog = page.locator(".v-dialog");
  await expect(importDialog).toBeVisible();
  await importDialog
    .getByRole("textbox", { name: "One server link per line:" })
    .fill(nodeLink);
  await importDialog.getByRole("button", { name: "Import" }).click();

  const card = nodeCard(page, "e2e-node");
  await expect(card).toBeVisible();

  // The share link is the round trip: the fields must survive import and
  // the generated link, or another client builds a tunnel this one cannot
  // read.
  await card.getByRole("button", { name: "Operations" }).click();
  await page.getByText("Share", { exact: true }).click();
  const shareDialog = page.locator(".v-dialog");
  const link = shareDialog.locator("input");
  await expect(link).toHaveValue(/encryption=mlkem\.testonly/);
  await expect(link).toHaveValue(/pqv=verify-key/);
  await shareDialog.getByRole("button", { name: "Confirm" }).click();

  // The editor shows what was stored, not only what the link said.
  await card.getByRole("button", { name: "Operations" }).click();
  await page.getByText("Modify", { exact: true }).click();
  const editor = page.locator(".v-dialog");
  await expect(
    editor.getByLabel("Encryption (encryption)"),
  ).toHaveValue("mlkem.testonly");
  await expect(
    editor.getByLabel("ML-DSA-65 Verify Key (pqv)"),
  ).toHaveValue("verify-key");
  await editor.getByRole("button", { name: "Cancel" }).click();

  // A reload reads the node back from the database.
  await page.reload();
  await openDestination(page, "Proxies");
  await expect(nodeCard(page, "e2e-node")).toBeVisible();
});

test("every destination renders", async ({ page }) => {
  await page.goto("/");
  for (const label of [
    "Proxies",
    "Subscriptions",
    "Settings",
    "Logs",
    "Documentation",
    "Dashboard",
  ]) {
    await openDestination(page, label);
  }
  // The docs article is compiled at build time and inserted with v-html;
  // an empty body would mean the CSP or the Markdown transform dropped it.
  await openDestination(page, "Documentation");
  await expect(page.locator(".docs__body h1").first()).toBeVisible();
});
