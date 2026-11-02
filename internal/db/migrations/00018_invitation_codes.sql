-- +goose Up
-- Every invitation has a code (ADR-00028): eight characters from the 62 of
-- 0-9, A-Z and a-z, unique across all invitations, and the only way a link or
-- a page names an invitation. The integer id stays the internal key. The
-- household package draws codes for new invitations (internal/household).
--
-- SQLite cannot add a NOT NULL UNIQUE column, so the table is rebuilt (create
-- the new shape, copy, drop the old, rename), as 00008 does. Rows keep their
-- ids. Nothing references invitations, so no other table is touched.
--
-- Existing rows get a code drawn here, from SQLite's random(): a ChaCha20
-- stream the SQLite library seeds from /dev/urandom. The draw is the same
-- rejection sampling as the application's: a random byte below 248 (four
-- times 62) maps to alphabet[byte % 62] and any other byte is discarded, so
-- every character is equally likely. The
-- recursive CTE keeps drawing until each row has eight characters. A collision
-- between two drawn codes fails UNIQUE and aborts the migration rather than
-- inventing a fallback; at 47 bits a rerun is what would fix it.

CREATE TABLE invitations_new (
    id           INTEGER PRIMARY KEY,
    code         TEXT NOT NULL UNIQUE,
    kind         TEXT NOT NULL CHECK (kind IN ('join', 'household')),
    email        TEXT NOT NULL CHECK (email <> '' AND email = lower(email)),
    household_id INTEGER REFERENCES households (id) ON DELETE CASCADE,
    issued_by    TEXT NOT NULL CHECK (issued_by <> ''),
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    accepted_at  TEXT,
    revoked_at   TEXT,
    CHECK ((kind = 'join') = (household_id IS NOT NULL))
);

INSERT INTO invitations_new (id, code, kind, email, household_id, issued_by, created_at, expires_at, accepted_at, revoked_at)
WITH RECURSIVE draw (id, code, b) AS (
    SELECT id, '', random() & 255 FROM invitations
    UNION ALL
    SELECT id,
           code || CASE WHEN b < 248
                        THEN substr('0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz', b % 62 + 1, 1)
                        ELSE '' END,
           random() & 255
    FROM draw WHERE length(code) < 8
)
SELECT i.id, d.code, i.kind, i.email, i.household_id, i.issued_by, i.created_at, i.expires_at, i.accepted_at, i.revoked_at
FROM invitations i JOIN draw d ON d.id = i.id AND length(d.code) = 8;

DROP TABLE invitations;
ALTER TABLE invitations_new RENAME TO invitations;
CREATE INDEX invitations_email ON invitations (email);

-- +goose Down
-- Back to the shape 00007 made: the code column is dropped (rebuilt, rows and
-- ids kept). Links that named a code stop working.
CREATE TABLE invitations_old (
    id           INTEGER PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('join', 'household')),
    email        TEXT NOT NULL CHECK (email <> '' AND email = lower(email)),
    household_id INTEGER REFERENCES households (id) ON DELETE CASCADE,
    issued_by    TEXT NOT NULL CHECK (issued_by <> ''),
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    accepted_at  TEXT,
    revoked_at   TEXT,
    CHECK ((kind = 'join') = (household_id IS NOT NULL))
);
INSERT INTO invitations_old (id, kind, email, household_id, issued_by, created_at, expires_at, accepted_at, revoked_at)
SELECT id, kind, email, household_id, issued_by, created_at, expires_at, accepted_at, revoked_at FROM invitations;
DROP TABLE invitations;
ALTER TABLE invitations_old RENAME TO invitations;
CREATE INDEX invitations_email ON invitations (email);
