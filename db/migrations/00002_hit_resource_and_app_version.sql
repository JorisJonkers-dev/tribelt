-- Every Hit records what it fetched (resource) and which build served it (app_version); every
-- Content Release records the App Versions that served it and its experiment tags.
-- The App Version backfill is best effort, by the release tags' commit times: before v0.2.0
-- (2026-10-01 09:47:24 UTC) is 0.1.0, before v0.3.0 (10:47:07 UTC) is 0.2.0, later rows are 0.3.0.
-- Each deploy lagged its tag by minutes, so a few Hits around a cutover may carry the newer version.

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

UPDATE hits SET app_version = CASE
    WHEN ts < '2026-10-01 09:47:24+00' THEN '0.1.0'
    WHEN ts < '2026-10-01 10:47:07+00' THEN '0.2.0'
    ELSE '0.3.0'
END
WHERE app_version = '';

UPDATE outbound_clicks SET app_version = CASE
    WHEN ts < '2026-10-01 09:47:24+00' THEN '0.1.0'
    WHEN ts < '2026-10-01 10:47:07+00' THEN '0.2.0'
    ELSE '0.3.0'
END
WHERE app_version = '';

-- A release was served by every version whose window overlaps its first and last sighting.
UPDATE releases SET app_versions = ARRAY(
    SELECT w.version
    FROM (VALUES
        ('0.1.0', '-infinity'::timestamptz, '2026-10-01 09:47:24+00'::timestamptz),
        ('0.2.0', '2026-10-01 09:47:24+00'::timestamptz, '2026-10-01 10:47:07+00'::timestamptz),
        ('0.3.0', '2026-10-01 10:47:07+00'::timestamptz, 'infinity'::timestamptz)
    ) AS w (version, since, until)
    WHERE releases.first_seen_at < w.until AND releases.last_seen_at >= w.since
    ORDER BY w.since
)
WHERE app_version = '';

UPDATE releases SET app_version = app_versions[1] WHERE app_version = '' AND cardinality(app_versions) > 0;
