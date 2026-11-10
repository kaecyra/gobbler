-- +goose Up
-- A proposal's reason is only ever an agent's own words (ADR-00035, ADR-00048).
-- Until now a re-import's draft was stored with the sentence "Re-imported from
-- <host>" as its reason; History now builds that line from the re-import
-- marker and the saved document's source address, so the stored sentence is
-- cleared. A reason an agent gave a re-import draft does not begin with that
-- sentence, so it is kept.
UPDATE proposals SET reason = ''
WHERE json_extract(document, '$.reimport') IS NOT NULL
  AND reason LIKE 'Re-imported from %';

-- +goose Down
-- The cleared sentence cannot be told apart from a reason that was never set,
-- and the host it named is in the source address of the document it came from.
SELECT 1;
