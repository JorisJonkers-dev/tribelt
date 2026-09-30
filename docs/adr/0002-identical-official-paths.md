# Mirror paths copy the official site exactly, quirks included

Every Mirror Page lives at the same path as its Official Page, including the official site's inconsistencies: trailing hyphens (`/kenniscentrum/tri-flex-`), the English hub at `/en/knowledge-center` with articles under `/en/knowledge-centre/`, and the same 301s (`/transportbanden` to `/metalen-transportbanden`). A 1:1 mapping means any Tribelt URL maps to ours by swapping the host, and a route-manifest test built from the crawl can prove every official route exists. Cleaner slugs would score slightly better on "descriptive URLs" but break that mapping.

## Consequences

- Do not "fix" these paths in passing. A slug cleanup is its own Content Release, with 301s from the old paths, so its effect can be measured.
