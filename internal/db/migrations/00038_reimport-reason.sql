-- +goose Up
-- A proposal's reason is only ever an agent's own words (ADR-00035, ADR-00048).
-- Until now a re-import's draft was stored with the sentence "Re-imported from
-- <host>" as its reason; History now builds that line from the re-import
-- marker and the saved document's source address, so the stored sentence is
-- cleared. The filter matches only the exact form the app wrote: "Re-imported
-- from " (17 characters, case-sensitive) and then a host with no space in it.
-- An agent's own reason that happens to begin the same way, such as
-- "Re-imported from example.com because the steps changed", has a space after
-- the first word and is kept.
UPDATE proposals SET reason = ''
WHERE json_extract(document, '$.reimport') IS NOT NULL
  AND reason GLOB 'Re-imported from *'
  AND instr(substr(reason, 18), ' ') = 0;

-- +goose Down
-- The cleared sentence cannot be told apart from a reason that was never set,
-- and the host it named is in the source address of the document it came from.
SELECT 1;
