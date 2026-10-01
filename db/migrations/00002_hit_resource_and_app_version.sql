-- Every Hit records what it fetched (resource) and which build served it (app_version); every
-- Content Release records the app versions that served it and its experiment tags.
-- Backfill is best effort: v0.2.0 was tagged at 2026-10-01 09:47:24 UTC, so anything recorded
-- before that is attributed to 0.1.0 and anything after to 0.2.0 (the deploy lagged the tag by
-- minutes, so a handful of 0.1.0 Hits around the cutover may read 0.2.0).

-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '60s';

ALTER TABLE hits ADD COLUMN IF NOT EXISTS resource text NOT NULL DEFAULT 'page';
ALTER TABLE hits ADD COLUMN IF NOT EXISTS app_version text NOT NULL DEFAULT '';
ALTER TABLE outbound_clicks ADD COLUMN IF NOT EXISTS app_version text NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN IF NOT EXISTS app_version text NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN IF NOT EXISTS app_versions text [] NOT NULL DEFAULT '{}';
ALTER TABLE releases ADD COLUMN IF NOT EXISTS tags text [] NOT NULL DEFAULT '{}';

-- Images become Hits, served as format img.
ALTER TABLE hits DROP CONSTRAINT IF EXISTS hits_format_check;
ALTER TABLE hits ADD CONSTRAINT hits_format_check
CHECK (format IN ('html', 'md', 'txt', 'xml', 'img')) NOT VALID;
ALTER TABLE hits ADD CONSTRAINT hits_resource_check
CHECK (resource IN (
    'page', 'markdown', 'robots', 'llms', 'llms_full', 'sitemap', 'image', 'redirect', 'not_found', 'outbound'
)) NOT VALID;

-- Same rules as visits.ClassifyResource.
UPDATE hits SET resource = CASE
    WHEN status IN (301, 302, 307, 308) THEN 'redirect'
    WHEN status = 404 THEN 'not_found'
    WHEN format = 'md' THEN 'markdown'
    WHEN path = '/robots.txt' THEN 'robots'
    WHEN path = '/llms.txt' THEN 'llms'
    WHEN path = '/llms-full.txt' THEN 'llms_full'
    WHEN path = '/sitemap.xml' OR format = 'xml' THEN 'sitemap'
    WHEN path LIKE '/images/%' THEN 'image'
    ELSE 'page'
END;

UPDATE hits SET app_version = CASE WHEN ts < '2026-10-01 09:47:24+00' THEN '0.1.0' ELSE '0.2.0' END
WHERE app_version = '';

UPDATE outbound_clicks SET app_version = CASE WHEN ts < '2026-10-01 09:47:24+00' THEN '0.1.0' ELSE '0.2.0' END
WHERE app_version = '';

UPDATE releases SET
    app_version = CASE WHEN first_seen_at < '2026-10-01 09:47:24+00' THEN '0.1.0' ELSE '0.2.0' END,
    app_versions = CASE
        WHEN last_seen_at < '2026-10-01 09:47:24+00' THEN ARRAY['0.1.0']
        WHEN first_seen_at < '2026-10-01 09:47:24+00' THEN ARRAY['0.1.0', '0.2.0']
        ELSE ARRAY['0.2.0']
    END
WHERE app_version = '';
