import { expect, test } from "@playwright/test";

// Minimal RFC 4180 line parser: user-agents contain quoted commas.
function cells(line) {
  const out = [];
  let cur = "", quoted = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (quoted) {
      if (c === '"' && line[i + 1] === '"') { cur += '"'; i++; }
      else if (c === '"') quoted = false;
      else cur += c;
    } else if (c === '"') quoted = true;
    else if (c === ",") { out.push(cur); cur = ""; }
    else cur += c;
  }
  out.push(cur);
  return out;
}

// Needs the server with DEV_AUTH_BYPASS=1 so /stats/hits.csv is readable.
test("a page view sends the beacon and its Hit becomes beacon-confirmed human", async ({ page, request }) => {
  const beacon = page.waitForRequest((r) => r.url().endsWith("/b") && r.method() === "POST");
  await page.goto("/sectoren");
  await expect(page.locator("h1")).toHaveCount(1);

  const hitId = await page.locator("script[data-hit]").getAttribute("data-hit");
  expect(hitId).toMatch(/^[0-9a-f-]{36}$/);

  // The request body of a sendBeacon Blob is not exposed to Playwright; the database check below
  // proves it carried this hit id.
  await beacon;

  // Leaving the page flushes the engaged-time beacon too.
  await page.goto("/en");

  await expect
    .poll(
      async () => {
        const csv = await (await request.get("/stats/hits.csv?internal=1")).text();
        const lines = csv.split("\n");
        const header = cells(lines[0]);
        const row = lines.find((line) => line.startsWith(hitId));
        if (!row) return "missing";
        const values = cells(row.trim());
        const col = (name) => values[header.indexOf(name)];
        return `${col("visitor_kind")} ${col("beacon_confirmed")}`;
      },
      { timeout: 15_000, intervals: [500] },
    )
    .toBe("human true");
});
