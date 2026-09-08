-- The history generation: an opaque marker, one per repository, naming the
-- numbering of its changelog. A change cursor (the `seq` a client saved to
-- resume from) is resumable only under the generation it was read from.
-- Importing a repository directory into a database that holds no row for it
-- recreates the row under the same authority, and the imported history's
-- seqs restart below whatever cursors clients saved from the history the
-- database held before; a bare seq cannot tell the two apart (decision
-- record 0056).
--
-- The engine mints a fresh value whenever it writes the row: at registration
-- and at import. A restart and `repository rebuild` leave the row alone, so
-- the marker holds across both. Rows written before this migration get one
-- here, so a cursor saved before the upgrade is refused once and re-listed,
-- never resumed unverified.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS history_generation text;
UPDATE repositories
    SET history_generation = substr(md5(random()::text || clock_timestamp()::text || id), 1, 16)
    WHERE history_generation IS NULL;
ALTER TABLE repositories ALTER COLUMN history_generation SET NOT NULL;
