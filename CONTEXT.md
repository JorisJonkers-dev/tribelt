# Tribelt Mirror

A student test site that mirrors the page structure of Tribelt's website to
measure how search engines, AI assistants and crawlers discover content. The
site is not Tribelt's product and says so on every page.

## Language

### Site content

**Mirror Page**:
A page on this site that corresponds to one route on Tribelt's official site,
carrying adapted (not verbatim) text and attribution to Tribelt.
_Avoid_: clone, copy page

**Official Page**:
The page on tribelt.nl that a Mirror Page corresponds to. Same path, other host.
_Avoid_: source page, original

**Locale**:
One of `nl` (default, no path prefix), `en` or `de`. Each Mirror Page exists
per Locale with its own translated path, as on the official site.

**Outbound Click**:
A visitor following a Mirror Page's link to its Official Page (quote,
contact, application). The mirror's conversion metric; the mirror has no forms.
_Avoid_: conversion, lead

**Content Release**:
One published state of all Mirror Pages, started deliberately by giving it a
new human label (e.g. `v3-longtail-keywords`) and a note on what changed.
Stats compare releases.
_Avoid_: version, deploy, build

### Visits

**Hit**:
One recorded request for a Mirror Page or agent-facing file, stored without
raw IP.
_Avoid_: pageview, event, request

**Visitor Kind**:
Who made a Hit: `human`, `human-unconfirmed`, `search-crawler`, `ai-crawler`,
`ai-fetcher`, `seo-tool` or `other-bot`.
_Avoid_: visitor type, agent type

**Human**:
A Hit from a browser that the beacon confirmed ran on the page.

**Human-unconfirmed**:
A Hit with a browser user-agent that never sent a beacon; may be a human
without JavaScript or a disguised bot.

**AI Crawler**:
An automated agent collecting content for model training or indexing
(GPTBot, ClaudeBot, CCBot).

**AI Fetcher**:
An agent fetching a page on behalf of a person's live question
(ChatGPT-User, Perplexity-User, Claude-User). Distinct from an AI Crawler.

**Arrival Channel**:
How a Human reached the page: `search`, `ai-chat`, `social`, `campaign`,
`referral`, `direct` or `internal`.
_Avoid_: source, medium, traffic type

**Visitor**:
A browser identified by a random first-party analytics ID that lives up to
13 months. Carries no personal data and is never shared.
_Avoid_: user, unique user, session

**Returning Visitor**:
A Visitor with Hits on more than one day.

**First-touch Channel**:
The Arrival Channel of a Visitor's first recorded Hit.

**Daily Visitor**:
A fallback identity for clients without a Visitor ID (bots, GPC/DNT, no
cookies): a hash with a salt that rotates each Amsterdam day and is never
stored. Not linkable across days.
_Avoid_: session

**Internal Hit**:
A Hit made by a signed-in Stats Viewer; recorded but hidden by default.

**Verified Crawler**:
A Hit whose claimed crawler identity matches that operator's published
address ranges. A claimed crawler that fails the check is `other-bot`.

**Search Performance**:
Per-page, per-day query, impression, click and position figures reported by
Google Search Console and Bing Webmaster Tools. Not derived from Hits.
_Avoid_: rankings, SEO data

### Access

**Stats Viewer**:
An auth-api admin, or a person holding the Tribelt stats service permission.

## Relationships

- A **Hit** belongs to exactly one **Mirror Page** (or agent-facing file) and one **Content Release**
- A **Hit** has exactly one **Visitor Kind**
- A **Hit** has an **Arrival Channel** only when its **Visitor Kind** is `human` or `human-unconfirmed`
- A **Hit** belongs to a **Visitor** when the client holds a Visitor ID, otherwise to a **Daily Visitor**
- A **Visitor** has exactly one **First-touch Channel**
- A **Daily Visitor** makes one or more **Hits** within a single day

## Flagged ambiguities

- "version" meant both a site-wide state and a per-page revision; resolved: only **Content Release** exists.
