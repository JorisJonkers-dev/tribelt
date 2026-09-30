---
id: privacyverklaring
locale: en
path: /en/privacy-statement
official: https://www.tribelt.nl/en/privacy-statement
type: privacy
title: "Privacy and Cookies on This Student Test Site"
description: "How this student test site measures visits: one first-party analytics cookie, no stored IP addresses, country only, no sharing, deleted at project end."
keywords: [privacy statement, analytics cookie, first-party cookie, GDPR, Global Privacy Control, student research project]
h1: "Privacy statement"
cta: { label: "Read Tribelt's own privacy statement on tribelt.nl", href: "https://www.tribelt.nl/en/privacy-statement" }
noindex: false
---
## Who this statement is about

This privacy statement covers **this website only**. The site is a student test site built for a university project on digital strategy. It mirrors the page structure of Tribelt's website, with Tribelt's permission, to measure how search engines, AI assistants and crawlers find and use content. It is **not** Tribelt's official website, and Tribelt B.V. does not receive or process the data described here. For how Tribelt itself handles personal data, read the privacy statement on tribelt.nl via the link on this page.

The student project team running this site is responsible for the processing described below. Contact: **contact@jorisjonkers.dev**.

## What we record

Every request to this site (pages, their Markdown versions, sitemap, robots.txt and llms.txt files) is recorded as a *hit* for aggregate statistics. For each hit we store:

- the date and time, the page requested, its language version and the content release it belonged to
- the format served (HTML, Markdown, text or XML) and the response status
- the full user-agent string your browser or bot sends, used to tell browsers from crawlers
- whether the visitor is a person or a bot, and for bots which crawler it claims to be and whether that claim checks out against the operator's published address ranges
- how a person arrived (for example via a search engine, an AI chat assistant, social media, a campaign link, another website or directly), derived from the referring website's host name and any `utm_` campaign parameters in the link
- the **country** you are visiting from, as reported by our network provider; nothing more precise than country level
- how long the page stayed open (engaged time), sent by a small script when the page loads and when you leave it
- a Visitor ID or a Daily Visitor hash, explained below

We do **not** store your IP address. The address is used only for a moment while the request is handled, to verify a claimed crawler and to compute the Daily Visitor hash, and is then discarded. We do not collect names, email addresses or any other information you type in: this site has no forms. Links to Tribelt's quote, contact and application pages are counted as outbound clicks before you are forwarded to tribelt.nl.

## The Visitor cookie

To see how many people return, the site sets one cookie named **`tv`**. It contains a random identifier and nothing else. The cookie is:

- first-party and host-only: it is set by this site and sent only to this site
- marked Secure, HttpOnly and SameSite=Lax
- kept for at most **13 months**
- used only to produce aggregate statistics, never for advertising or profiling, and never shared

We do not show a cookie banner. Dutch law exempts analytics cookies that have little or no impact on your privacy from the consent requirement (Article 11.7a(3)(b) of the Telecommunicatiewet, the Dutch Telecommunications Act). This site uses no third-party cookies, no advertising trackers and no external analytics services.

## If you send Global Privacy Control or Do Not Track

If your browser sends a Global Privacy Control signal (`Sec-GPC: 1`) or a Do Not Track signal, we do **not** set the `tv` cookie. The same applies to all bots and crawlers. For those requests we compute a **Daily Visitor** hash instead: a keyed hash of the IP address and user-agent using a secret salt that changes every day (Amsterdam time) and is held only in memory, never stored. The hash lets us count visits within a single day, but it cannot be linked across days and cannot be turned back into an IP address.

If you have blocked cookies, the same fallback applies.

## Search statistics

We also import aggregate figures from Google Search Console and Bing Webmaster Tools, such as how often each page appeared in search results and was clicked. These figures are reported per page, query and day by the search engines and contain no information about individual visitors.

## Signing in to the statistics

Only project members can view the statistics. They sign in through a separate login service, which sets a session cookie for them only. Visits by signed-in project members are marked as internal and excluded from the figures by default. Ordinary visitors never sign in and never receive this cookie.

## Sharing and storage

We do not sell, share or publish the recorded data. Results may appear in the project report only as aggregate figures that cannot be traced to a person. The data are stored on a server under the project team's control and are kept **until the project ends**, after which they are deleted. The site is delivered through a network provider (Cloudflare) that processes requests in order to deliver the pages and to report the visitor's country.

## Your rights

Under the General Data Protection Regulation you may ask for access to, correction of or deletion of data relating to you, and you may object to the processing. Because we store no names or IP addresses, we can only find your hits if you give us the value of your `tv` cookie. You can delete that cookie in your browser at any time, which ends the link between your future and past visits. To exercise your rights, email **contact@jorisjonkers.dev**. You may also lodge a complaint with the Dutch Data Protection Authority (Autoriteit Persoonsgegevens).

## Changes

If the way this site measures visits changes, this statement will be updated before the change takes effect.
