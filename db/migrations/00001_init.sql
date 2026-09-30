-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS releases (
    label text PRIMARY KEY,
    note text NOT NULL,
    content_hash text NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pages (
    release_label text NOT NULL REFERENCES releases (label),
    path text NOT NULL,
    page_id text NOT NULL,
    locale text NOT NULL,
    type text NOT NULL,
    title text NOT NULL,
    description text NOT NULL,
    h1 text NOT NULL,
    keywords text [] NOT NULL DEFAULT '{}',
    word_count bigint NOT NULL,
    content_hash text NOT NULL,
    PRIMARY KEY (release_label, path)
);

CREATE TABLE IF NOT EXISTS hits (
    id uuid PRIMARY KEY,
    ts timestamptz NOT NULL,
    path text NOT NULL,
    page_id text,
    locale text,
    release_label text NOT NULL,
    format text NOT NULL CHECK (format IN ('html', 'md', 'txt', 'xml')),
    status bigint NOT NULL,
    visitor_kind text NOT NULL CHECK (
        visitor_kind IN ('human', 'human-unconfirmed', 'search-crawler', 'ai-crawler', 'ai-fetcher', 'seo-tool', 'other-bot')
    ),
    bot_name text,
    verified boolean,
    arrival_channel text CHECK (
        arrival_channel IN ('search', 'ai-chat', 'social', 'campaign', 'referral', 'direct', 'internal')
    ),
    referrer_host text,
    referrer_name text,
    utm_source text,
    utm_medium text,
    utm_campaign text,
    utm_term text,
    utm_content text,
    country text,
    user_agent text NOT NULL,
    visitor_id text,
    daily_hash text NOT NULL,
    internal boolean NOT NULL DEFAULT false,
    beacon_confirmed boolean NOT NULL DEFAULT false,
    engaged_ms bigint
);

-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS hits_ts_idx ON hits (ts);
-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS hits_path_ts_idx ON hits (path, ts);
-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS hits_visitor_idx ON hits (visitor_id, ts) WHERE visitor_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS outbound_clicks (
    id uuid PRIMARY KEY,
    ts timestamptz NOT NULL,
    from_path text NOT NULL,
    page_id text,
    locale text,
    release_label text NOT NULL,
    target text NOT NULL,
    visitor_kind text NOT NULL,
    bot_name text,
    verified boolean,
    visitor_id text,
    daily_hash text NOT NULL,
    internal boolean NOT NULL DEFAULT false,
    country text
);

-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS outbound_clicks_ts_idx ON outbound_clicks (ts);

CREATE TABLE IF NOT EXISTS search_performance (
    source text NOT NULL CHECK (source IN ('google', 'bing')),
    day date NOT NULL,
    page text NOT NULL,
    path text NOT NULL,
    query text NOT NULL,
    clicks bigint NOT NULL,
    impressions bigint NOT NULL,
    ctr double precision NOT NULL,
    position double precision NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, day, page, query)
);

-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS search_performance_path_day_idx ON search_performance (path, day);
