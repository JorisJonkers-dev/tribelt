-- Every stats query shares one filter: [from_ts, to_ts), optional locale, release label, app version and
-- release tag, and the internal toggle. Image Hits only count in the resource breakdowns.

-- name: KindTotals :many
SELECT visitor_kind, count(*) AS hits, count(DISTINCT COALESCE(visitor_id, daily_hash)) AS visitors
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND resource <> 'image'
GROUP BY visitor_kind
ORDER BY visitor_kind;

-- name: ChannelTotals :many
SELECT COALESCE(arrival_channel, 'direct')::text AS channel, count(*) AS hits,
       count(DISTINCT COALESCE(visitor_id, daily_hash)) AS visitors,
       count(*) FILTER (WHERE beacon_confirmed) AS confirmed
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND visitor_kind IN ('human', 'human-unconfirmed') AND format = 'html'
GROUP BY 1
ORDER BY 2 DESC;

-- name: DailyKinds :many
SELECT (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day, visitor_kind, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND (sqlc.narg(path)::text IS NULL OR (CASE WHEN resource = 'markdown' AND path = '/index.md' THEN '/' WHEN resource = 'markdown' THEN regexp_replace(path, '\.md$', '') ELSE path END) = sqlc.narg(path))
  AND resource <> 'image'
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: DailyHumanVisitors :many
SELECT (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day, count(DISTINCT COALESCE(visitor_id, daily_hash)) AS visitors
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND visitor_kind IN ('human', 'human-unconfirmed') AND resource IN ('page', 'markdown')
GROUP BY 1
ORDER BY 1;

-- name: VisitorSummary :one
WITH h AS (
    SELECT visitor_id, daily_hash, (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day
    FROM hits
    WHERE ts >= @from_ts AND ts < @to_ts
      AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
      AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
      AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
      AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
      AND (@include_internal::boolean OR NOT internal)
      AND visitor_kind IN ('human', 'human-unconfirmed') AND resource IN ('page', 'markdown')
)
SELECT
    (SELECT count(DISTINCT visitor_id) FROM h WHERE visitor_id IS NOT NULL) AS visitors,
    (SELECT count(*) FROM (
        SELECT visitor_id FROM h WHERE visitor_id IS NOT NULL GROUP BY visitor_id HAVING count(DISTINCT day) > 1
    ) AS r) AS returning_visitors,
    (SELECT count(DISTINCT (daily_hash, day)) FROM h WHERE visitor_id IS NULL) AS daily_visitors;

-- name: FirstTouchChannels :many
WITH first_hits AS (
    SELECT DISTINCT ON (visitor_id) visitor_id, ts, arrival_channel
    FROM hits
    WHERE visitor_id IS NOT NULL AND visitor_kind IN ('human', 'human-unconfirmed') AND resource IN ('page', 'markdown')
      AND (@include_internal::boolean OR NOT internal)
    ORDER BY visitor_id, ts
)
SELECT COALESCE(arrival_channel, 'direct')::text AS channel, count(*) AS visitors
FROM first_hits
WHERE ts >= @from_ts AND ts < @to_ts
GROUP BY 1
ORDER BY 2 DESC;

-- name: OutboundTotals :many
SELECT visitor_kind, count(*) AS clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY visitor_kind
ORDER BY visitor_kind;

-- name: DailyOutbound :many
SELECT (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day, count(*) AS clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY 1
ORDER BY 1;

-- name: OutboundBreakdown :many
SELECT visitor_kind, COALESCE(bot_name, '')::text AS bot_name, target, count(*) AS clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY 1, 2, 3
ORDER BY 4 DESC
LIMIT 500;

-- name: PageTable :many
-- A Markdown twin (/x.md, /index.md) counts towards its page's path.
SELECT (CASE WHEN resource = 'markdown' AND path = '/index.md' THEN '/' WHEN resource = 'markdown' THEN regexp_replace(path, '\.md$', '') ELSE path END)::text AS path, max(page_id)::text AS page_id, max(locale)::text AS locale,
       count(*) FILTER (WHERE visitor_kind = 'human') AS human,
       count(*) FILTER (WHERE visitor_kind = 'human-unconfirmed') AS human_unconfirmed,
       count(*) FILTER (WHERE visitor_kind = 'search-crawler') AS search_crawler,
       count(*) FILTER (WHERE visitor_kind = 'ai-crawler') AS ai_crawler,
       count(*) FILTER (WHERE visitor_kind = 'ai-fetcher') AS ai_fetcher,
       count(*) FILTER (WHERE visitor_kind IN ('seo-tool', 'other-bot')) AS other,
       count(*) FILTER (WHERE format = 'md') AS markdown,
       COALESCE(avg(engaged_ms) FILTER (WHERE beacon_confirmed), 0)::bigint AS avg_engaged_ms
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND page_id IS NOT NULL
GROUP BY 1
ORDER BY human DESC, human_unconfirmed DESC, 1;

-- name: DailyPathHumans :many
SELECT path, (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND path = ANY (@paths::text [])
  AND visitor_kind IN ('human', 'human-unconfirmed')
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: OutboundByPage :many
SELECT from_path, count(*) AS clicks, count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed')) AS human_clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY from_path;

-- name: PageReferrers :many
SELECT COALESCE(arrival_channel, 'direct')::text AS channel, COALESCE(referrer_host, '')::text AS referrer_host, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND path = @path
  AND visitor_kind IN ('human', 'human-unconfirmed')
GROUP BY 1, 2
ORDER BY 3 DESC
LIMIT 50;

-- name: BotTable :many
SELECT COALESCE(bot_name, '')::text AS bot_name, visitor_kind,
       count(*) AS hits, count(*) FILTER (WHERE verified) AS verified, count(*) FILTER (WHERE verified IS NOT NULL) AS checked,
       count(DISTINCT path) FILTER (WHERE page_id IS NOT NULL) AS pages,
       count(*) FILTER (WHERE resource = 'page') AS page,
       count(*) FILTER (WHERE resource = 'markdown') AS markdown,
       count(*) FILTER (WHERE resource = 'robots') AS robots,
       count(*) FILTER (WHERE resource IN ('llms', 'llms_full')) AS llms,
       count(*) FILTER (WHERE resource = 'sitemap') AS sitemap,
       count(*) FILTER (WHERE resource IN ('image', 'redirect', 'not_found')) AS other,
       max(ts)::timestamptz AS last_seen
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND (sqlc.narg(path)::text IS NULL OR (CASE WHEN resource = 'markdown' AND path = '/index.md' THEN '/' WHEN resource = 'markdown' THEN regexp_replace(path, '\.md$', '') ELSE path END) = sqlc.narg(path))
  AND visitor_kind NOT IN ('human', 'human-unconfirmed')
GROUP BY 1, 2
ORDER BY hits DESC, bot_name;

-- name: BotKinds :many
SELECT visitor_kind, count(*) AS hits, count(*) FILTER (WHERE verified) AS verified,
       count(DISTINCT path) FILTER (WHERE page_id IS NOT NULL) AS pages
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND visitor_kind NOT IN ('human', 'human-unconfirmed') AND resource <> 'image'
GROUP BY visitor_kind
ORDER BY hits DESC;

-- name: ResourceKinds :many
SELECT resource, visitor_kind, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: ResourceTopClients :many
SELECT DISTINCT ON (resource) resource, client, hits
FROM (
    SELECT resource,
           (CASE WHEN visitor_kind IN ('human', 'human-unconfirmed') THEN 'Browsers' ELSE COALESCE(bot_name, 'unknown') END)::text AS client,
           count(*) AS hits
    FROM hits
    WHERE ts >= @from_ts AND ts < @to_ts
      AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
      AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
      AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
      AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
      AND (@include_internal::boolean OR NOT internal)
    GROUP BY 1, 2
) AS c
ORDER BY resource, hits DESC, client;

-- name: ResourceTopPaths :many
SELECT DISTINCT ON (resource) resource, path, hits
FROM (
    SELECT resource, path, count(*) AS hits
    FROM hits
    WHERE ts >= @from_ts AND ts < @to_ts
      AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
      AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
      AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
      AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
      AND (@include_internal::boolean OR NOT internal)
    GROUP BY 1, 2
) AS p
ORDER BY resource, hits DESC, path;

-- name: AgentFileClients :many
SELECT resource, (CASE WHEN visitor_kind IN ('human', 'human-unconfirmed') THEN 'Browsers' ELSE COALESCE(bot_name, 'unknown') END)::text AS client,
       count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND resource IN ('robots', 'llms', 'llms_full', 'markdown')
GROUP BY 1, 2
ORDER BY 3 DESC, 2;

-- name: MarkdownPages :one
SELECT count(DISTINCT path) AS pages
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND resource = 'markdown';

-- name: AIFetcherPaths :many
SELECT path, format, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND visitor_kind = 'ai-fetcher'
GROUP BY 1, 2
ORDER BY 3 DESC, 1
LIMIT 10;

-- name: ReleaseTotals :many
SELECT release_label,
       count(DISTINCT (ts AT TIME ZONE 'Europe/Amsterdam')::date) AS days,
       count(*) FILTER (WHERE visitor_kind = 'human') AS human,
       count(*) FILTER (WHERE visitor_kind = 'human-unconfirmed') AS human_unconfirmed,
       count(*) FILTER (WHERE visitor_kind = 'search-crawler') AS search_crawler,
       count(*) FILTER (WHERE visitor_kind = 'ai-crawler') AS ai_crawler,
       count(*) FILTER (WHERE visitor_kind = 'ai-fetcher') AS ai_fetcher,
       count(*) FILTER (WHERE visitor_kind IN ('seo-tool', 'other-bot')) AS other,
       count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed') AND arrival_channel = 'search') AS from_search,
       count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed') AND arrival_channel = 'ai-chat') AS from_ai_chat,
       count(*) FILTER (WHERE visitor_kind NOT IN ('human', 'human-unconfirmed') AND resource = 'markdown') AS markdown,
       count(*) FILTER (WHERE visitor_kind NOT IN ('human', 'human-unconfirmed') AND resource IN ('llms', 'llms_full')) AS llms,
       count(DISTINCT visitor_id) AS visitors
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
  AND resource <> 'image'
GROUP BY release_label;

-- name: OutboundByRelease :many
SELECT release_label, count(*) AS clicks, count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed')) AS human_clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(version)::text IS NULL OR app_version = sqlc.narg(version))
  AND (sqlc.narg(tag)::text IS NULL OR release_label IN (SELECT r.label FROM releases AS r WHERE sqlc.narg(tag)::text = ANY (r.tags)))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY release_label;
