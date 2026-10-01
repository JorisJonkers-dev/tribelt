-- name: InsertHit :exec
INSERT INTO hits (
    id, ts, path, page_id, locale, release_label, format, status, visitor_kind, bot_name, verified,
    arrival_channel, referrer_host, referrer_name, utm_source, utm_medium, utm_campaign, utm_term, utm_content,
    country, user_agent, visitor_id, daily_hash, internal, resource, app_version
) VALUES (
    @id, @ts, @path, @page_id, @locale, @release_label, @format, @status, @visitor_kind, @bot_name, @verified,
    @arrival_channel, @referrer_host, @referrer_name, @utm_source, @utm_medium, @utm_campaign, @utm_term, @utm_content,
    @country, @user_agent, @visitor_id, @daily_hash, @internal, @resource, @app_version
);

-- name: ConfirmBeacon :execrows
UPDATE hits
SET beacon_confirmed = true,
    engaged_ms = GREATEST(COALESCE(engaged_ms, 0), @engaged_ms::bigint),
    visitor_kind = 'human'
WHERE id = @id
  AND visitor_kind IN ('human', 'human-unconfirmed')
  AND ts > @not_before;

-- name: InsertOutboundClick :exec
INSERT INTO outbound_clicks (
    id, ts, from_path, page_id, locale, release_label, target, visitor_kind, bot_name, verified,
    visitor_id, daily_hash, internal, country, app_version
) VALUES (
    @id, @ts, @from_path, @page_id, @locale, @release_label, @target, @visitor_kind, @bot_name, @verified,
    @visitor_id, @daily_hash, @internal, @country, @app_version
);

-- name: GetHit :one
SELECT * FROM hits WHERE id = @id;
