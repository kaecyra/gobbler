-- +goose Up
-- Profiles (ADR-00032): one row per person who has opened their profile, keyed
-- by the email as every other table stores it (lower-cased, as in 00007). The
-- profile package owns the SQL (internal/profile).
--
-- address is the page's opaque random name: 16 random bytes in unpadded
-- base64url, which is 22 characters. It never contains the email, so an address
-- reveals and lets nobody guess an email. display_name is empty until the person
-- sets one, and then the page shows the email's local part. avatar is the
-- reference to an uploaded picture and stays empty until one is uploaded; the
-- default avatar is drawn from the email and never stored. Timestamps are
-- RFC 3339 UTC text.

CREATE TABLE profiles (
    email        TEXT NOT NULL PRIMARY KEY CHECK (email <> '' AND email = lower(email)),
    address      TEXT NOT NULL UNIQUE CHECK (length(address) = 22),
    display_name TEXT NOT NULL DEFAULT '',
    avatar       TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- +goose Down
DROP TABLE profiles;
