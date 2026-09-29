import { chromium } from "@playwright/test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
const base = process.env.OFFSCRIPT_PREVIEW_URL || "http://127.0.0.1:8098";
const out = ".impeccable/review";
await mkdir(out, { recursive: true });
const browser = await chromium.launch({
  headless: true,
  args: ["--no-sandbox"],
});
const errors = [];
for (const [name, viewport] of [
  ["desktop", { width: 1440, height: 1000 }],
  ["mobile", { width: 390, height: 844 }],
]) {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  page.on("pageerror", (e) => errors.push(name + ": " + e.message));
  async function visit(path) {
    const r = await page.goto(base + path);
    assert.equal(r.status(), 200, path);
    await page.waitForLoadState("networkidle");
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      true,
      "overflow: " + name + " " + path,
    );
  }
  await visit("/");
  await page.keyboard.press("Tab");
  assert.equal(
    await page
      .locator(".skip-link")
      .evaluate((e) => e === document.activeElement),
    true,
    "skip link",
  );
  await page.locator("body").click({ position: { x: 1, y: 1 } });
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: true });
  const story = await page.locator(".story a").first().getAttribute("href");
  await visit(story);
  assert.ok(await page.locator(".article-body").textContent());
  await page.screenshot({ path: `${out}/${name}-article.png`, fullPage: true });
  await visit("/topics");
  await page.locator(".term-list a").first().click();
  await page.waitForLoadState("networkidle");
  assert.ok(await page.locator(".story").count());
  await visit("/authors");
  await visit("/search?q=room");
  assert.ok(await page.locator(".story").count());
  await visit("/search?q=nomatchingstory");
  assert.ok(await page.locator(".empty").count());
  await visit("/register");
  await visit("/forgot-password");
  await visit("/login");
  await page.screenshot({ path: `${out}/${name}-login.png`, fullPage: true });
  await page.getByLabel("Email address").fill("reader@example.test");
  await page.getByLabel("Password").fill("offscript-preview-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL("**/feed");
  await visit(story);
  await Promise.all([
    page.waitForNavigation({ waitUntil: "networkidle" }),
    page.getByRole("button", { name: /Save story|Saved/, exact: true }).click(),
  ]);
  await visit("/saved");
  await visit("/topics");
  await page.locator(".term-list a").first().click();
  await page.getByRole("button", { name: /^Follow/ }).click();
  await visit("/feed");
  await visit(story);
  await page
    .getByLabel("Your comment")
    .fill("A thoughtful test comment from the " + name + " journey.");
  await Promise.all([
    page.waitForNavigation({ waitUntil: "networkidle" }),
    page.getByRole("button", { name: "Publish comment" }).click(),
  ]);
  assert.ok((await page.locator(".comment").count()) >= 1);
  await visit("/notifications");
  await visit("/settings");
  await page.screenshot({
    path: `${out}/${name}-settings.png`,
    fullPage: true,
  });
  const admin = await context.request.get(base + "/api/admin/members");
  assert.equal(admin.status(), 401);
  await page.locator(".account-menu summary").click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page.waitForURL(base + "/");
  await visit("/login");
  await page.getByLabel("Email address").fill("admin@example.test");
  await page.getByLabel("Password").fill("offscript-preview-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL("**/feed");
  for (const path of [
    "/admin",
    "/admin/members",
    "/admin/moderation",
    "/admin/authors",
    "/admin/topics",
    "/admin/newsletters",
  ])
    await visit(path);
  await visit("/admin/editor");
  await page.waitForSelector(".tiptap");
  await page
    .getByLabel("Title", { exact: true })
    .fill(`Browser verified draft ${name} ${Date.now()}`);
  await page
    .getByLabel("Summary", { exact: true })
    .fill("A complete draft created through the real formatted editor.");
  await page.getByLabel("Author byline").fill("Avery Reed");
  await page.getByLabel("Topic", { exact: true }).fill("Ideas");
  await page
    .locator(".tiptap")
    .fill(
      "A new original story written in the formatted editor, with enough text to save and publish.",
    );
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await page.waitForFunction(
    () =>
      document.querySelector("#save-status")?.textContent === "Draft saved.",
  );
  await page.locator("body").click({ position: { x: 1, y: 1 } });
  await page.screenshot({ path: `${out}/${name}-editor.png`, fullPage: true });
  assert.match(page.url(), /\/admin\/editor\/.+/);
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  assert.ok(await page.locator("#preview").isVisible());
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelector("#save-status")?.textContent === "Published.",
  );
  await page.getByRole("button", { name: "Unpublish", exact: true }).click();
  await page.waitForFunction(
    () =>
      document.querySelector("#save-status")?.textContent === "Draft saved.",
  );
  await context.close();
  console.log(`${name}: guest, reader, admin flows passed`);
}
assert.deepEqual(errors, []);
await browser.close();
console.log("No JavaScript errors; desktop/mobile pages fit the viewport.");
