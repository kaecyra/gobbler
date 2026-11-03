-- +goose Up
-- What a session records about the browser that started it (ADR-00032): the
-- user agent and the client IP address, shown to the person on their profile so
-- they can tell their sessions apart and end the ones they do not know.
-- Existing sessions have neither, so both default to empty. The IP is for
-- display only and is never used to admit or refuse anything: behind the tunnel
-- it is the CF-Connecting-IP header, which anything that reaches the port
-- directly can set (ADR-00027). Both live and die with the row.

ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN client_ip TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN client_ip;
ALTER TABLE sessions DROP COLUMN user_agent;
