-- +goose Up
-- Invitation email (ADR-00055): when an invitation's email last went out and
-- why the last attempt failed. Both are NULL until a send is attempted, which
-- is also all an invitation made with no mail configured ever has. emailed_at
-- is the last send that succeeded, in the same fixed-width text as the other
-- invitation times; email_error is the last attempt's error and is cleared by
-- the next success. The household package owns the SQL (internal/household).
ALTER TABLE invitations ADD COLUMN emailed_at TEXT;
ALTER TABLE invitations ADD COLUMN email_error TEXT;

-- +goose Down
ALTER TABLE invitations DROP COLUMN email_error;
ALTER TABLE invitations DROP COLUMN emailed_at;
