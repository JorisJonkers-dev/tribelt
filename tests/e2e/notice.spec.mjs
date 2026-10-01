import { expect, test } from "@playwright/test";

// Records whether <html> already had the notice-on class when <body> was first inserted, i.e. before paint.
function watchBodyInsert() {
  new MutationObserver((records, observer) => {
    if (!document.body) return;
    window.__noticeOnAtBody = document.documentElement.classList.contains("notice-on");
    observer.disconnect();
  }).observe(document, { childList: true, subtree: true });
}

const notice = (page) => page.locator("aside.notice");

test("a fresh visitor sees the notice pill, linking to the Official Page", async ({ page }) => {
  await page.addInitScript(watchBodyInsert);
  await page.goto("/transportbanden/draadogenbanden");
  await expect(notice(page)).toBeVisible();
  expect(await page.evaluate(() => window.__noticeOnAtBody)).toBe(true);
  const href = await notice(page).getByRole("link").getAttribute("href");
  expect(decodeURIComponent(href)).toContain("to=https://www.tribelt.nl/transportbanden/draadogenbanden");
  const close = notice(page).getByRole("button", { name: "Melding sluiten" });
  const box = await close.boundingBox();
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.height).toBeGreaterThanOrEqual(44);
});

test("a dismissed pill stays gone across pages and visits, without a flash", async ({ page, context }) => {
  await page.goto("/");
  await notice(page).getByRole("button").click();
  await expect(notice(page)).toBeHidden();

  await page.addInitScript(watchBodyInsert);
  await page.goto("/en");
  await expect(page.locator("h1")).toHaveCount(1);
  await expect(notice(page)).toBeHidden();
  expect(await page.evaluate(() => window.__noticeOnAtBody)).toBe(false);

  // A new tab in the same browser profile is a later visit.
  const later = await context.newPage();
  await later.goto("/de");
  await expect(notice(later)).toBeHidden();
});

test("a fresh context shows the pill again", async ({ browser }) => {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto(`${test.info().project.use.baseURL}/sectoren`);
  await expect(notice(page)).toBeVisible();
  await context.close();
});

test("without localStorage the dismissal falls back to sessionStorage", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(window, "localStorage", { get() { throw new DOMException("blocked", "SecurityError"); } });
  });
  await page.goto("/");
  await notice(page).getByRole("button").click();
  await page.goto("/contact");
  await expect(page.locator("h1")).toHaveCount(1);
  await expect(notice(page)).toBeHidden();
});

test("without JavaScript the pill never shows; the top bar does", async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  await page.goto(`${test.info().project.use.baseURL}/`);
  await expect(page.locator("aside.testbar")).toBeVisible();
  await expect(notice(page)).toBeHidden();
  await context.close();
});
