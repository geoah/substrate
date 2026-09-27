-- Which rule set indexed a repository's rows (engine/searchindex.go). `fts`
-- is derived from each row and the kind's declaration by rules the binary
-- carries (ftsBands, searchText), so a binary whose rules differ from the ones
-- that indexed the rows re-derives every row's `fts` at the repository's next
-- open and records the version here. No row reads as version 1, the rules
-- before this table existed.
--
-- Repository-scoped like every table a dataset writes: the `repository`
-- column defaults to the connection's setting and row level security holds
-- each row to it.
CREATE TABLE IF NOT EXISTS search_index (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    version     integer     NOT NULL,
    indexed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);
ALTER TABLE search_index ENABLE ROW LEVEL SECURITY;
ALTER TABLE search_index FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS search_index_repository ON search_index;
CREATE POLICY search_index_repository ON search_index FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered the tables that existed then,
-- so a later table carries its own. The app role rewrites its one row on each
-- reindex; erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE ON TABLE %I.search_index TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.search_index TO substrate_maint', sch);
    END IF;
END $$;
