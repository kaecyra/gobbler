-- +goose Up
-- Households as tenants (ADR-00018): a household has several members, an email
-- belongs to at most one household, and nobody joins or starts a household
-- without an invitation addressed to their email. The household package owns
-- the SQL (internal/household). Scoping recipe, shopping and catalog rows to a
-- household is the next nodes' migrations, not this one.
--
-- Emails are stored trimmed and lower-cased by the application; the CHECK
-- keeps a path that skips that from creating a second row for one person.
-- Timestamps are RFC 3339 UTC text.

CREATE TABLE households (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    created_at TEXT NOT NULL
);

-- UNIQUE (email) is the one-household-per-person rule, enforced by the schema.
-- There are no roles: every member has the same powers.
CREATE TABLE household_members (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    email        TEXT NOT NULL UNIQUE CHECK (email <> '' AND email = lower(email)),
    joined_at    TEXT NOT NULL
);
CREATE INDEX household_members_household_id ON household_members (household_id);

-- A join invitation names the household it adds the invitee to; a household
-- invitation (issued by the operator) names none, and accepting it creates the
-- household. accepted_at makes it single use, revoked_at lets the issuer
-- withdraw it, and expires_at bounds how long it can be accepted.
CREATE TABLE invitations (
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
CREATE INDEX invitations_email ON invitations (email);

-- A recipe copy sent from one household to another (used by the sharing
-- node). The document and source snapshot are taken at send time so the
-- recipient's copy is independent of later edits on the sender's side.
CREATE TABLE recipe_shares (
    id                     INTEGER PRIMARY KEY,
    sender_household_id    INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipient_household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    sent_by                TEXT NOT NULL CHECK (sent_by <> ''),
    recipe_document        TEXT NOT NULL,
    source_snapshot        TEXT,
    status                 TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined')),
    created_at             TEXT NOT NULL,
    resolved_at            TEXT
);
CREATE INDEX recipe_shares_recipient ON recipe_shares (recipient_household_id, status);

-- +goose Down
DROP TABLE recipe_shares;
DROP TABLE invitations;
DROP TABLE household_members;
DROP TABLE households;
