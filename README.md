# Tribelt mirror

A student test site that mirrors the page structure of [tribelt.nl](https://www.tribelt.nl) at
`tribelt.jorisjonkers.dev`, to measure how search engines, AI assistants and crawlers discover
content. It is made with Tribelt's permission and says on every page that it is not Tribelt's site.
Vocabulary: [CONTEXT.md](CONTEXT.md). Decisions: [docs/adr](docs/adr).

One Go binary serves everything:

- **Mirror Pages** (NL/EN/DE, the official paths and 301s), Markdown twins (`<path>.md` or
  `Accept: text/markdown`), `sitemap.xml`, `robots.txt`, `llms.txt`, `llms-full.txt`. All of it is
  rendered once at startup from `content/`, which is embedded in the binary.
- **Hits** for every public request, classified by Visitor Kind, Verified Crawler and Arrival Channel,
  written asynchronously to Postgres (a page never waits on the database). `/b` is the beacon,
  `/go` records an Outbound Click and redirects to tribelt.nl.
- **`/stats`** for Stats Viewers (OIDC against auth-api): Overview, Per page, Release compare,
  AI & crawlers; CSV/SVG/PNG exports and an anonymised SQLite download.
- A daily **Search Performance** import from Google Search Console and Bing Webmaster Tools, off
  when their credentials are empty.

```
cmd/tribelt            main: serve (default), migrate
cmd/update-ranges      refreshes internal/visits/ranges (task update-ranges)
content/               site.yml, redirects.yml, pages/<locale>/<id>.md, images/
internal/content       load + validate content, render HTML, JSON-LD, sitemap, robots, llms, twins
internal/site          serves the pre-rendered resources, redirects, negotiation, 404s
internal/visits        pure classification (Visitor Kind, crawler ranges, Arrival Channel, identities)
internal/hits          tracking middleware, async writer, /b beacon, /go outbound
internal/stats         stats views, charts, CSV, SQLite export, Search Performance import
internal/release       records the running Content Release and page snapshots
internal/platform      config, pg (goose + sqlc), oidc, session, httpx
db/migrations, db/queries, web/templates, web/static, tests/e2e, platform/
```

## Local development

Tooling comes from [mise](https://mise.jdx.dev): `mise install` once, then everything is a
[Task](https://taskfile.dev) target (`task` lists them).

```bash
task dev          # compose Postgres on :5433 + the site on :8080, stats open via DEV_AUTH_BYPASS=1
task check        # what CI runs: fmt, lint (golangci, squawk, actionlint), sqlc drift, vet,
                  # race tests (testcontainers Postgres), coverage gates, release gate, build, gitleaks
task e2e          # Playwright: a real page view's beacon confirms its Hit
task gen          # sqlc after editing db/queries or db/migrations
task update-ranges  # refresh the bundled crawler IP ranges
```

`task dev` reads `content/` from disk (`CONTENT_DIR=content`), so a content edit only needs a
restart. `DEV_AUTH_BYPASS=1` opens `/stats` without sign-in and is refused when
`TRIBELT_ENV=production`. There is no localhost redirect registered at auth-api, so the real OIDC
flow is exercised by the fake-issuer tests, not locally.

Try it:

```bash
curl -s localhost:8080/robots.txt
curl -s -H 'Accept: text/markdown' localhost:8080/sectoren
curl -s -A 'Mozilla/5.0 (compatible; GPTBot/1.2; +https://openai.com/gptbot)' \
     -H 'CF-Connecting-IP: 132.196.86.5' localhost:8080/sectoren >/dev/null   # a Verified Crawler Hit
open http://localhost:8080/stats
```

## Editing content

Every Mirror Page is one Markdown file, `content/pages/<locale>/<id>.md`. The front matter holds
what search engines and assistants read first:

| field | what it does | rule |
| --- | --- | --- |
| `title` | `<title>`, OG and JSON-LD name | unique, at most 60 characters, not the official title |
| `description` | meta description, llms.txt summary | unique, 120 to 160 characters |
| `keywords` | meta keywords, stored per release for comparison | list |
| `h1` | the one H1; the body starts at `##` | required |
| `image` | lead image with `alt` (required) and `credit` | file under `content/images/` |
| `product` | spec table + Product JSON-LD `additionalProperty` | only on `type: product` |
| `faq` | `<details>` accordion + FAQPage JSON-LD | `q` and `a` both required |
| `cta` | the tracked link to the Official Page | `https://www.tribelt.nl/...` only |

`id`, `locale`, `path` and `official` are identity: the path equals the Official Page's path,
quirks included (ADR-0002), and pages sharing an `id` are one hreflang group.

Body text is adapted, never copied: the test suite fails any passage sharing 25 consecutive words
with the tribelt.nl crawl, any duplicate title or description, a second H1, an image without alt
text, or an internal link that goes nowhere. Loading is strict, so a typo in a front matter key
fails the build instead of silently disappearing.

**Every content change is a new Content Release.** Change `release` in `content/site.yml`:

```yaml
release: { label: v2-longtail-keywords, note: "Product titles lead with the long-tail term" }
```

The label is a lowercase slug and must be new; CI (`task release-gate`) fails a change under
`content/` that keeps the old label. The running release is recorded at startup, stamped on every
Hit, drawn as a marker on every timeline and compared in *Release compare*. Sitemap `lastmod` is
the date a page's current text first went live, so unchanged pages keep their date.

## Environment

| variable | meaning |
| --- | --- |
| `PGHOST` `PGPORT` `PGDATABASE` `PGUSER` `PGPASSWORD` `PGSSLMODE` | Postgres (libpq variables; `DATABASE_URL` overrides, for development) |
| `AUTO_MIGRATE` | `true` (default) runs goose migrations on start; `tribelt migrate` runs them alone |
| `BASE_URL` | public origin; https turns on Secure cookies and HSTS |
| `TRIBELT_ENV` | `production` demands OIDC, `VISITOR_HMAC_KEY` and https, and refuses the bypass |
| `OIDC_ISSUER` `OIDC_CLIENT_ID` `OIDC_CLIENT_SECRET` `OIDC_REDIRECT_URL` | stats sign-in (discovery, PKCE, state, nonce) |
| `SESSION_KEY` | at least 32 characters; encrypts the `__Host-` session cookie |
| `VISITOR_HMAC_KEY` | derives the daily Daily Visitor salt |
| `GSC_SERVICE_ACCOUNT_JSON` `GSC_SITE_URL` | Search Console import; empty = off |
| `BING_API_KEY` `BING_SITE_URL` | Bing Webmaster import; empty = off |
| `DEV_AUTH_BYPASS` | `1` opens `/stats` in development only |
| `CONTENT_DIR` | read content from disk instead of the embedded copy |

Stats access is granted at auth-api's authorize endpoint (service permission `TRIBELT`) and
checked again here: the ID token's `roles` must contain `SERVICE_TRIBELT` or `ROLE_ADMIN`.

## Deploying tribelt.jorisjonkers.dev

The image embeds `content/`, so a Content Release ships as a new image.

1. Tag the release (`vX.Y.Z`) and run **Publish** (manual until onboarding is complete). It builds
   `ghcr.io/jorisjonkers-dev/tribelt` for linux/amd64 and linux/arm64, pins the digest in
   `platform/images.lock.json` and publishes the deploy artifact from `platform/`.
2. The first time only, make the GHCR package public.
3. A follow-up pull request in `fleet-infra` pins the new digest. The Deployment and IngressRoute
   are hand-written there, not rendered from `platform/deployment.yml`; secrets arrive via
   `envFrom`, non-secret values are listed in `platform/production.env`.
4. After Flux applies it, check `https://tribelt.jorisjonkers.dev/healthz` (liveness, also used by
   gatus) and `/readyz`, then read the new release label back from `/stats`.
