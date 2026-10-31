-- +goose Up
-- port of java crawler V1..V4. fixes: unique site name, cascades, enum checks,
-- site_id on urls (per-site queues), last_error, one item per url (upsert), crawl_run replaces spring batch.

CREATE TABLE site (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    base_url            TEXT        NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),
    enabled             BOOLEAN     NOT NULL DEFAULT TRUE,
    politeness_delay_ms INTEGER     NOT NULL DEFAULT 1000 CHECK (politeness_delay_ms BETWEEN 0 AND 600000),
    extraction_config   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT site_name_key UNIQUE (name)
);

CREATE TABLE category (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    site_id              BIGINT      NOT NULL REFERENCES site(id) ON DELETE CASCADE,
    name                 TEXT        NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    url                  TEXT        NOT NULL CHECK (length(url) BETWEEN 1 AND 2048),
    pagination_type      TEXT        NOT NULL CHECK (pagination_type IN ('PAGE_PARAM', 'CURSOR', 'SCROLL_AJAX')),
    item_link_selector   TEXT        NOT NULL CHECK (length(item_link_selector) BETWEEN 1 AND 500),
    next_page_selector   TEXT        CHECK (length(next_page_selector) <= 500),
    link_extraction_type TEXT        NOT NULL DEFAULT 'HREF' CHECK (link_extraction_type IN ('HREF', 'ONCLICK', 'DATA_ATTRIBUTE')),
    link_attribute       TEXT        CHECK (length(link_attribute) <= 255),
    enabled              BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT category_site_name_key UNIQUE (site_id, name),
    CONSTRAINT category_link_attribute_required CHECK (link_extraction_type <> 'DATA_ATTRIBUTE' OR link_attribute IS NOT NULL)
);

CREATE TABLE discovered_url (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    site_id         BIGINT      NOT NULL REFERENCES site(id) ON DELETE CASCADE,
    category_id     BIGINT      NOT NULL REFERENCES category(id) ON DELETE CASCADE,
    url             TEXT        NOT NULL CHECK (length(url) BETWEEN 1 AND 2048),
    url_hash        CHAR(64)    NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'IN_PROGRESS', 'COMPLETED', 'FAILED')),
    retry_count     INTEGER     NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    last_attempt_at TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT discovered_url_hash_key UNIQUE (url_hash)
);
CREATE INDEX discovered_url_site_status_idx ON discovered_url (site_id, status, id);
CREATE INDEX discovered_url_category_idx ON discovered_url (category_id);

CREATE TABLE extracted_item (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    discovered_url_id BIGINT      NOT NULL REFERENCES discovered_url(id) ON DELETE CASCADE,
    site_id           BIGINT      NOT NULL REFERENCES site(id) ON DELETE CASCADE,
    title             TEXT        NOT NULL,
    properties        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    raw_snapshot_path TEXT,
    extracted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT extracted_item_url_key UNIQUE (discovered_url_id)
);
CREATE INDEX extracted_item_site_extracted_idx ON extracted_item (site_id, extracted_at DESC, id DESC);
CREATE INDEX extracted_item_extracted_idx ON extracted_item (extracted_at DESC, id DESC);

-- one row per discover/extract/crawl request. status driven by worker.
CREATE TABLE crawl_run (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    site_id     BIGINT      NOT NULL REFERENCES site(id) ON DELETE CASCADE,
    category_id BIGINT      REFERENCES category(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('DISCOVER_CATEGORY', 'DISCOVER_SITE', 'EXTRACT_SITE', 'CRAWL_SITE')),
    status      TEXT        NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED', 'RUNNING', 'COMPLETED', 'FAILED')),
    stats       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);
CREATE INDEX crawl_run_site_idx ON crawl_run (site_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE crawl_run;
DROP TABLE extracted_item;
DROP TABLE discovered_url;
DROP TABLE category;
DROP TABLE site;
