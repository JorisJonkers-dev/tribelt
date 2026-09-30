// Runs against an already started server (task e2e / CI start it): E2E_BASE_URL, default :18080.
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  retries: 0,
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: process.env.E2E_BASE_URL || "http://localhost:18080",
    // A normal browser UA: headless Chromium's "HeadlessChrome" token is classified as other-bot.
    userAgent:
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36",
  },
});
