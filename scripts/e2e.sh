#!/usr/bin/env bash
# Starts the built binary against compose Postgres (stats bypass on) and runs the Playwright smoke test.
set -euo pipefail

port="${E2E_PORT:-18080}"
export E2E_BASE_URL="http://localhost:${port}"
DATABASE_URL="${DATABASE_URL:-postgres://tribelt:tribelt@localhost:5433/tribelt_db?sslmode=disable}" \
  DEV_AUTH_BYPASS=1 TRIBELT_ADDR=":${port}" bin/tribelt serve > /tmp/tribelt-e2e.log 2>&1 &
server=$!
trap 'kill "${server}" 2>/dev/null || true' EXIT
for _ in $(seq 1 60); do
  curl -sf "${E2E_BASE_URL}/readyz" >/dev/null && break
  sleep 1
done
cd tests/e2e
npm ci --no-audit --no-fund
npx playwright install chromium
npx playwright test
