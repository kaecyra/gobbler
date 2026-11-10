-- +goose Up
-- A draft's picture candidates are chosen by an ID of their own, not by
-- position (ADR-00052). offer is the candidate's opaque ID, a random token
-- never reused, as recipe_found_pictures.offer is for the edit page
-- (ADR-00051): the review form chooses by it, so a later fetch of the draft's
-- candidates cannot put a different picture behind a choice already on the
-- page. position still orders the picker. New candidates are kept under
-- drafts/<proposal_id>/<offer>.webp; a candidate held before this migration
-- keeps the key its row names.
CREATE TABLE draft_candidates_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    proposal_id  INTEGER NOT NULL,
    offer        TEXT NOT NULL UNIQUE CHECK (offer <> ''),
    position     INTEGER NOT NULL CHECK (position >= 1),
    source_url   TEXT NOT NULL,
    key          TEXT NOT NULL CHECK (key <> ''),
    width        INTEGER NOT NULL CHECK (width >= 1),
    height       INTEGER NOT NULL CHECK (height >= 1),
    UNIQUE (proposal_id, position),
    FOREIGN KEY (household_id, proposal_id) REFERENCES proposals (household_id, id) ON DELETE CASCADE
);
-- 11 random bytes in hex are 22 characters of the offer ID's alphabet, the
-- shape picture.ValidOfferID accepts.
INSERT INTO draft_candidates_new (id, household_id, proposal_id, offer, position, source_url, key, width, height)
SELECT id, household_id, proposal_id, lower(hex(randomblob(11))), position, source_url, key, width, height
FROM draft_candidates;

-- A draft document's picture choice was a number: a candidate's position, -1
-- for No picture or -2 for Keep current picture. It is now a string: the
-- candidate's offer, "none" or "keep". A position with no candidate behind it
-- (the draft is finished and its candidates deleted) was no choice to keep.
UPDATE proposals
SET document = json_set(document, '$.picture', (
    SELECT c.offer FROM draft_candidates_new c
    WHERE c.household_id = proposals.household_id AND c.proposal_id = proposals.id
      AND c.position = json_extract(proposals.document, '$.picture')))
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'integer'
  AND json_extract(document, '$.picture') > 0
  AND EXISTS (
    SELECT 1 FROM draft_candidates_new c
    WHERE c.household_id = proposals.household_id AND c.proposal_id = proposals.id
      AND c.position = json_extract(proposals.document, '$.picture'));
UPDATE proposals SET document = json_set(document, '$.picture', 'none')
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'integer' AND json_extract(document, '$.picture') = -1;
UPDATE proposals SET document = json_set(document, '$.picture', 'keep')
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'integer' AND json_extract(document, '$.picture') = -2;
UPDATE proposals SET document = json_remove(document, '$.picture')
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'integer';

DROP TABLE draft_candidates;
ALTER TABLE draft_candidates_new RENAME TO draft_candidates;
CREATE INDEX draft_candidates_household ON draft_candidates (household_id);

-- +goose Down
UPDATE proposals
SET document = json_set(document, '$.picture', (
    SELECT c.position FROM draft_candidates c
    WHERE c.household_id = proposals.household_id AND c.proposal_id = proposals.id
      AND c.offer = json_extract(proposals.document, '$.picture')))
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'text'
  AND EXISTS (
    SELECT 1 FROM draft_candidates c
    WHERE c.household_id = proposals.household_id AND c.proposal_id = proposals.id
      AND c.offer = json_extract(proposals.document, '$.picture'));
UPDATE proposals SET document = json_set(document, '$.picture', -1)
WHERE kind = 'draft' AND json_extract(document, '$.picture') = 'none';
UPDATE proposals SET document = json_set(document, '$.picture', -2)
WHERE kind = 'draft' AND json_extract(document, '$.picture') = 'keep';
UPDATE proposals SET document = json_remove(document, '$.picture')
WHERE kind = 'draft' AND json_type(document, '$.picture') = 'text';

CREATE TABLE draft_candidates_old (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    proposal_id  INTEGER NOT NULL,
    position     INTEGER NOT NULL CHECK (position >= 1),
    source_url   TEXT NOT NULL,
    key          TEXT NOT NULL CHECK (key <> ''),
    width        INTEGER NOT NULL CHECK (width >= 1),
    height       INTEGER NOT NULL CHECK (height >= 1),
    UNIQUE (proposal_id, position),
    FOREIGN KEY (household_id, proposal_id) REFERENCES proposals (household_id, id) ON DELETE CASCADE
);
INSERT INTO draft_candidates_old (id, household_id, proposal_id, position, source_url, key, width, height)
SELECT id, household_id, proposal_id, position, source_url, key, width, height FROM draft_candidates;
DROP TABLE draft_candidates;
ALTER TABLE draft_candidates_old RENAME TO draft_candidates;
CREATE INDEX draft_candidates_household ON draft_candidates (household_id);
