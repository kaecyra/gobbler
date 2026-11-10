-- Integration settings (ADR-00057). Per deployment: no household column.

-- name: GetSetting :one
SELECT secret, value FROM integration_settings WHERE integration = ? AND field = ?;

-- name: PutSetting :exec
INSERT INTO integration_settings (integration, field, secret, value, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (integration, field) DO UPDATE
SET secret = excluded.secret, value = excluded.value, updated_at = excluded.updated_at;

-- name: DeleteSetting :exec
DELETE FROM integration_settings WHERE integration = ? AND field = ?;

-- name: ListSecrets :many
SELECT integration, field, value FROM integration_settings WHERE secret = 1 ORDER BY integration, field;
