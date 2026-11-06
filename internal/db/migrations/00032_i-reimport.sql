-- +goose Up
-- A recipe may hold more than one source snapshot of a kind (ADR-00042): a
-- re-import fetches the source again and attaches the result beside the first,
-- which stays as the original. Readers that want one snapshot of a kind take
-- the newest. SQLite cannot drop a constraint, so the table is rebuilt with
-- the same columns, checks and foreign key, minus UNIQUE (recipe_id, kind), and
-- an index that serves the "newest of a kind" read.
--
-- Dropping the old table deletes its rows first, which runs the ON DELETE SET
-- NULL of proposals.snapshot_id and would unlink every waiting draft from its
-- snapshot. The links are saved and put back; ids are kept by the copy.
CREATE TEMP TABLE proposal_snapshot_links AS
SELECT id AS proposal_id, snapshot_id FROM proposals WHERE snapshot_id IS NOT NULL;

CREATE TABLE source_snapshots_new (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id      INTEGER,
    kind           TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content        BLOB NOT NULL,
    sha256         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    created_by     TEXT NOT NULL CHECK (created_by <> ''),
    parser_version TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
INSERT INTO source_snapshots_new (id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version)
SELECT id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots;
DROP TABLE source_snapshots;
ALTER TABLE source_snapshots_new RENAME TO source_snapshots;
CREATE INDEX source_snapshots_recipe_kind ON source_snapshots (recipe_id, kind, id);

UPDATE proposals SET snapshot_id = (SELECT l.snapshot_id FROM proposal_snapshot_links l WHERE l.proposal_id = proposals.id)
WHERE id IN (SELECT proposal_id FROM proposal_snapshot_links);
DROP TABLE proposal_snapshot_links;

-- +goose Down
-- Only one snapshot of a kind can exist again: the first, the original, is
-- kept and the ones a re-import attached are dropped. A draft that pointed at
-- one loses the link (SET NULL), as when its snapshot is pruned.
CREATE TEMP TABLE proposal_snapshot_links AS
SELECT id AS proposal_id, snapshot_id FROM proposals WHERE snapshot_id IS NOT NULL;

DELETE FROM source_snapshots
WHERE recipe_id IS NOT NULL
  AND id NOT IN (SELECT MIN(id) FROM source_snapshots WHERE recipe_id IS NOT NULL GROUP BY recipe_id, kind);
DROP INDEX source_snapshots_recipe_kind;
CREATE TABLE source_snapshots_old (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id      INTEGER,
    kind           TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content        BLOB NOT NULL,
    sha256         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    created_by     TEXT NOT NULL CHECK (created_by <> ''),
    parser_version TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, kind),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
INSERT INTO source_snapshots_old (id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version)
SELECT id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots;
DROP TABLE source_snapshots;
ALTER TABLE source_snapshots_old RENAME TO source_snapshots;

UPDATE proposals SET snapshot_id = (SELECT l.snapshot_id FROM proposal_snapshot_links l WHERE l.proposal_id = proposals.id
    AND l.snapshot_id IN (SELECT id FROM source_snapshots))
WHERE id IN (SELECT proposal_id FROM proposal_snapshot_links);
DROP TABLE proposal_snapshot_links;
