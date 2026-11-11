-- LLM calls, prices and the price sync (ADR-00058). Per deployment: the
-- household is a column of the record, not a scope of the query.

-- name: InsertCall :exec
INSERT INTO llm_calls (called_at, task, provider, model, household_id, input_tokens, output_tokens, latency_ms, outcome, error, cost_micros)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPrice :one
SELECT provider, model, input_micros, output_micros, source, synced_at, updated_at
FROM llm_prices WHERE provider = ? AND model = ?;

-- name: ListPrices :many
SELECT provider, model, input_micros, output_micros, source, synced_at, updated_at
FROM llm_prices ORDER BY provider, model;

-- UpsertSyncedPrice sets a price from the sync. A manual price stays: the
-- conflict update applies only to a row the sync set itself.
-- name: UpsertSyncedPrice :execrows
INSERT INTO llm_prices (provider, model, input_micros, output_micros, source, synced_at, updated_at)
VALUES (?, ?, ?, ?, 'openrouter', ?, ?)
ON CONFLICT (provider, model) DO UPDATE
SET input_micros = excluded.input_micros, output_micros = excluded.output_micros,
    synced_at = excluded.synced_at, updated_at = excluded.updated_at
WHERE llm_prices.source = 'openrouter';

-- name: SetManualPrice :exec
INSERT INTO llm_prices (provider, model, input_micros, output_micros, source, synced_at, updated_at)
VALUES (?, ?, ?, ?, 'manual', NULL, ?)
ON CONFLICT (provider, model) DO UPDATE
SET input_micros = excluded.input_micros, output_micros = excluded.output_micros,
    source = 'manual', synced_at = NULL, updated_at = excluded.updated_at;

-- name: DeletePrice :exec
DELETE FROM llm_prices WHERE provider = ? AND model = ?;

-- UsedModels are the models any call has been recorded for.
-- name: UsedModels :many
SELECT DISTINCT provider, model FROM llm_calls ORDER BY provider, model;

-- name: GetSyncStatus :one
SELECT last_success_at, last_failure_at, last_error FROM llm_price_sync WHERE id = 1;

-- name: RecordSyncSuccess :exec
INSERT INTO llm_price_sync (id, last_success_at) VALUES (1, ?)
ON CONFLICT (id) DO UPDATE SET last_success_at = excluded.last_success_at;

-- name: RecordSyncFailure :exec
INSERT INTO llm_price_sync (id, last_failure_at, last_error) VALUES (1, ?, ?)
ON CONFLICT (id) DO UPDATE SET last_failure_at = excluded.last_failure_at, last_error = excluded.last_error;

-- name: Summary :one
SELECT COUNT(*) AS calls,
       CAST(COALESCE(SUM(input_tokens), 0) AS INTEGER) AS input_tokens,
       CAST(COALESCE(SUM(output_tokens), 0) AS INTEGER) AS output_tokens,
       CAST(COALESCE(SUM(outcome = 'error'), 0) AS INTEGER) AS failed,
       CAST(COALESCE(SUM(cost_micros), 0) AS INTEGER) AS cost_micros,
       CAST(COALESCE(SUM(cost_micros IS NULL), 0) AS INTEGER) AS unpriced
FROM llm_calls WHERE called_at >= ?;

-- name: SummaryByModel :many
SELECT task, provider, model,
       COUNT(*) AS calls,
       CAST(COALESCE(SUM(input_tokens), 0) AS INTEGER) AS input_tokens,
       CAST(COALESCE(SUM(output_tokens), 0) AS INTEGER) AS output_tokens,
       CAST(COALESCE(SUM(outcome = 'error'), 0) AS INTEGER) AS failed,
       CAST(COALESCE(SUM(cost_micros), 0) AS INTEGER) AS cost_micros,
       CAST(COALESCE(SUM(cost_micros IS NULL), 0) AS INTEGER) AS unpriced
FROM llm_calls WHERE called_at >= ?
GROUP BY task, provider, model
ORDER BY task, provider, model;

-- name: SummaryByHousehold :many
SELECT c.household_id AS household_id,
       CAST(COALESCE(h.name, '') AS TEXT) AS household_name,
       COUNT(*) AS calls,
       CAST(COALESCE(SUM(c.input_tokens), 0) AS INTEGER) AS input_tokens,
       CAST(COALESCE(SUM(c.output_tokens), 0) AS INTEGER) AS output_tokens,
       CAST(COALESCE(SUM(c.outcome = 'error'), 0) AS INTEGER) AS failed,
       CAST(COALESCE(SUM(c.cost_micros), 0) AS INTEGER) AS cost_micros,
       CAST(COALESCE(SUM(c.cost_micros IS NULL), 0) AS INTEGER) AS unpriced
FROM llm_calls c LEFT JOIN households h ON h.id = c.household_id
WHERE c.called_at >= ?
GROUP BY c.household_id
ORDER BY COALESCE(h.name, ''), c.household_id;

-- name: RecentFailures :many
SELECT c.called_at, c.task, c.provider, c.model,
       CAST(COALESCE(h.name, '') AS TEXT) AS household_name,
       c.latency_ms, CAST(COALESCE(c.error, '') AS TEXT) AS error
FROM llm_calls c LEFT JOIN households h ON h.id = c.household_id
WHERE c.outcome = 'error' AND c.called_at >= ?
ORDER BY c.id DESC
LIMIT ?;
