-- Every stats query shares one filter: [from_ts, to_ts), optional locale and release, internal toggle.

-- name: KindTotals :many
SELECT visitor_kind, count(*) AS hits, count(DISTINCT COALESCE(visitor_id, daily_hash)) AS visitors
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (@include_internal::boolean OR NOT internal)
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
  AND (@include_internal::boolean OR NOT internal)
  AND (sqlc.narg(path)::text IS NULL OR path = sqlc.narg(path))
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: VisitorSummary :one
WITH h AS (
    SELECT visitor_id, daily_hash, (ts AT TIME ZONE 'Europe/Amsterdam')::date AS day
    FROM hits
    WHERE ts >= @from_ts AND ts < @to_ts
      AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
      AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
      AND (@include_internal::boolean OR NOT internal)
      AND visitor_kind IN ('human', 'human-unconfirmed')
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
    WHERE visitor_id IS NOT NULL AND visitor_kind IN ('human', 'human-unconfirmed')
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
  AND (@include_internal::boolean OR NOT internal)
GROUP BY visitor_kind
ORDER BY visitor_kind;

-- name: PageTable :many
SELECT path, max(page_id)::text AS page_id, max(locale)::text AS locale,
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
  AND (@include_internal::boolean OR NOT internal)
  AND page_id IS NOT NULL
GROUP BY path
ORDER BY human DESC, human_unconfirmed DESC, path;

-- name: OutboundByPage :many
SELECT from_path, count(*) AS clicks, count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed')) AS human_clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY from_path;

-- name: PageReferrers :many
SELECT COALESCE(arrival_channel, 'direct')::text AS channel, COALESCE(referrer_host, '')::text AS referrer_host, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts AND path = @path
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (@include_internal::boolean OR NOT internal)
  AND visitor_kind IN ('human', 'human-unconfirmed')
GROUP BY 1, 2
ORDER BY 3 DESC
LIMIT 50;

-- name: BotTable :many
SELECT COALESCE(bot_name, '')::text AS bot_name, visitor_kind, COALESCE(verified, false)::boolean AS verified,
       count(*) AS hits, count(DISTINCT path) AS paths,
       count(*) FILTER (WHERE format = 'md') AS markdown, count(*) FILTER (WHERE format = 'txt') AS txt,
       count(*) FILTER (WHERE format = 'xml') AS xml, max(ts)::timestamptz AS last_seen
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND (sqlc.narg(path)::text IS NULL OR path = sqlc.narg(path))
  AND visitor_kind NOT IN ('human', 'human-unconfirmed')
GROUP BY 1, 2, 3
ORDER BY hits DESC;

-- name: AgentTopPaths :many
SELECT path, count(*) AS hits,
       count(*) FILTER (WHERE visitor_kind = 'ai-crawler') AS ai_crawler,
       count(*) FILTER (WHERE visitor_kind = 'ai-fetcher') AS ai_fetcher,
       count(*) FILTER (WHERE visitor_kind = 'search-crawler') AS search_crawler
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (sqlc.narg(release)::text IS NULL OR release_label = sqlc.narg(release))
  AND visitor_kind IN ('ai-crawler', 'ai-fetcher', 'search-crawler')
GROUP BY path
ORDER BY hits DESC
LIMIT 50;

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
       count(DISTINCT visitor_id) AS visitors
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY release_label;

-- name: OutboundByRelease :many
SELECT release_label, count(*) AS clicks, count(*) FILTER (WHERE visitor_kind IN ('human', 'human-unconfirmed')) AS human_clicks
FROM outbound_clicks
WHERE ts >= @from_ts AND ts < @to_ts
  AND (sqlc.narg(locale)::text IS NULL OR locale = sqlc.narg(locale))
  AND (@include_internal::boolean OR NOT internal)
GROUP BY release_label;
