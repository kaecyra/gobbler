-- +goose Up
-- LLM usage and cost (ADR-00058). The internal/llmusage package owns the SQL.
--
-- llm_calls holds one row per call made to a provider, successful or not.
-- Timestamps are RFC 3339 UTC text. household_id is the household the call ran
-- for, NULL when it ran for none. Deleting a household deletes its calls with
-- the rest of its data (ADR-00018, Members). Tokens are as the provider reports them (0 on a failed
-- call, which reports none). cost_micros is millionths of a US dollar, computed
-- when the call was recorded from the price then in force; NULL means the model
-- had no price, which is not a cost of zero. A later price change never
-- rewrites it.
CREATE TABLE llm_calls (
    id            INTEGER PRIMARY KEY,
    called_at     TEXT NOT NULL,
    task          TEXT NOT NULL CHECK (task IN ('text', 'vision')),
    provider      TEXT NOT NULL CHECK (provider <> ''),
    model         TEXT NOT NULL CHECK (model <> ''),
    household_id  INTEGER REFERENCES households (id) ON DELETE CASCADE,
    input_tokens  INTEGER NOT NULL CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL CHECK (output_tokens >= 0),
    latency_ms    INTEGER NOT NULL CHECK (latency_ms >= 0),
    outcome       TEXT NOT NULL CHECK (outcome IN ('ok', 'error')),
    error         TEXT,
    cost_micros   INTEGER CHECK (cost_micros >= 0),
    CHECK ((outcome = 'error') = (error IS NOT NULL))
);

CREATE INDEX llm_calls_called_at ON llm_calls (called_at);
CREATE INDEX llm_calls_household ON llm_calls (household_id);

-- llm_prices holds the price of one model of one provider: input and output,
-- each in millionths of a US dollar per million tokens (so $3.00 per million
-- is 3000000). source says where it came from: 'openrouter' is set by the
-- daily sync and may be replaced by it, 'manual' was set by the operator and
-- the sync never touches it. synced_at is when the sync last set the price,
-- NULL for a manual one.
CREATE TABLE llm_prices (
    provider     TEXT NOT NULL CHECK (provider <> ''),
    model        TEXT NOT NULL CHECK (model <> ''),
    input_micros  INTEGER NOT NULL CHECK (input_micros >= 0),
    output_micros INTEGER NOT NULL CHECK (output_micros >= 0),
    source       TEXT NOT NULL CHECK (source IN ('openrouter', 'manual')),
    synced_at    TEXT,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (provider, model),
    CHECK ((source = 'openrouter') = (synced_at IS NOT NULL))
) WITHOUT ROWID;

-- llm_price_sync is the sync's status: one row, id 1, once a sync has run.
-- last_success_at and last_failure_at are kept apart so the page shows both;
-- last_error is the failure's message, with no key in it.
CREATE TABLE llm_price_sync (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    last_success_at TEXT,
    last_failure_at TEXT,
    last_error      TEXT
);

-- +goose Down
DROP TABLE llm_price_sync;
DROP TABLE llm_prices;
DROP INDEX llm_calls_household;
DROP INDEX llm_calls_called_at;
DROP TABLE llm_calls;
