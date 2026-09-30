-- name: UpsertSearchPerformance :exec
INSERT INTO search_performance (source, day, page, path, query, clicks, impressions, ctr, position, imported_at)
VALUES (@source, @day, @page, @path, @query, @clicks, @impressions, @ctr, @position, now())
ON CONFLICT (source, day, page, query) DO UPDATE
SET path = excluded.path, clicks = excluded.clicks, impressions = excluded.impressions,
    ctr = excluded.ctr, position = excluded.position, imported_at = now();

-- name: SearchTotals :many
SELECT source, COALESCE(sum(clicks), 0)::bigint AS clicks, COALESCE(sum(impressions), 0)::bigint AS impressions,
       COALESCE(sum(position * impressions) / NULLIF(sum(impressions), 0), 0)::double precision AS avg_position
FROM search_performance
WHERE day >= @from_day AND day < @to_day
  AND (sqlc.narg(path)::text IS NULL OR path = sqlc.narg(path))
GROUP BY source
ORDER BY source;

-- name: SearchDaily :many
SELECT day, COALESCE(sum(clicks), 0)::bigint AS clicks, COALESCE(sum(impressions), 0)::bigint AS impressions
FROM search_performance
WHERE day >= @from_day AND day < @to_day
  AND (sqlc.narg(path)::text IS NULL OR path = sqlc.narg(path))
GROUP BY day
ORDER BY day;

-- name: SearchByPath :many
SELECT path, COALESCE(sum(clicks), 0)::bigint AS clicks, COALESCE(sum(impressions), 0)::bigint AS impressions,
       COALESCE(sum(position * impressions) / NULLIF(sum(impressions), 0), 0)::double precision AS avg_position
FROM search_performance
WHERE day >= @from_day AND day < @to_day
GROUP BY path;

-- name: TopQueries :many
SELECT query, source, COALESCE(sum(clicks), 0)::bigint AS clicks, COALESCE(sum(impressions), 0)::bigint AS impressions,
       COALESCE(sum(position * impressions) / NULLIF(sum(impressions), 0), 0)::double precision AS avg_position
FROM search_performance
WHERE day >= @from_day AND day < @to_day
  AND (sqlc.narg(path)::text IS NULL OR path = sqlc.narg(path))
GROUP BY query, source
ORDER BY impressions DESC, clicks DESC
LIMIT 50;

-- name: ListSearchPerformance :many
SELECT * FROM search_performance ORDER BY day, source, page, query;

-- name: ListOutboundClicks :many
SELECT * FROM outbound_clicks ORDER BY ts;
