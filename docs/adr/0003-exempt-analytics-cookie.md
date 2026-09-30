# Returning visitors via an exempt first-party analytics cookie, no consent banner

We set a random, first-party, host-only Visitor ID cookie (up to 13 months) without a consent banner, relying on the Dutch exemption for analytics cookies with little or no privacy impact (Telecommunicatiewet art. 11.7a(3)(b)). The ID is used only for aggregate statistics, is never shared, and is disclosed on the privacy page. A consent banner would have been the most conservative option, but typical opt-in rates of 30–60% would bias every visitor metric, and the banner itself costs page weight in an SEO experiment. A cookieless daily hash alone cannot see returning visitors or cross-day journeys, which the project needs.

## Consequences

- Clients sending GPC or DNT, and every bot, get no cookie; their Hits fall back to a Daily Visitor hash with a daily-rotating, never-stored salt.
- Raw IP addresses are never stored.
- If the exemption is judged not to apply (for example by the university or Tribelt), add a consent gate in front of the cookie; the Daily Visitor fallback already covers visitors without one.
