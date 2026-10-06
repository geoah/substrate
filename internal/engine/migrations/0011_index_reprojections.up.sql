-- The kinds whose derived indexes a declaration change left to be re-derived
-- behind the open (engine/reprojection.go). A row's refs rows and its `fts`
-- are each a function of the row's properties and its kind's declaration,
-- so a shipped declaration that moves a reference site or an `fts` flag
-- moves what the kind's stored rows derive to. Every request on a
-- repository waits for its open, so the boot upgrade re-derives no stored
-- row's indexes in its transaction: it writes one row here per reshaped
-- data kind, in that same transaction, and a pass behind the open
-- re-derives the rows that carry a moved property, records after each page
-- the id it ended at, and deletes the row when the kind is done. A pass a
-- shutdown interrupts finds its rows again at the next open and resumes.
--
-- `refs_properties` and `fts_properties` name, for the index that moved
-- (`refs`, `fts`), the kind's top-level properties whose declaration moved:
-- a row carrying none of them derives the same rows under either
-- declaration, so the pass reads only the rows that carry one. NULL means
-- every row of the kind, which is what a kind declared on one side only
-- needs; an index that did not move stores NULL too and is not read.
-- `after_id` is the id the last committed page ended at, empty before the
-- first page and whenever the request is widened.
--
-- Repository-scoped like every table a dataset writes: the `repository`
-- column defaults to the connection's setting and row level security holds
-- each row to it.
CREATE TABLE IF NOT EXISTS index_reprojections (
    repository      text        NOT NULL DEFAULT current_setting('substrate.repository'),
    kind            text        NOT NULL,
    refs            boolean     NOT NULL,
    refs_properties text[],
    fts             boolean     NOT NULL,
    fts_properties  text[],
    after_id        text        NOT NULL DEFAULT '',
    requested_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, kind)
);
ALTER TABLE index_reprojections ENABLE ROW LEVEL SECURITY;
ALTER TABLE index_reprojections FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS index_reprojections_repository ON index_reprojections;
CREATE POLICY index_reprojections_repository ON index_reprojections FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered the tables that existed then,
-- so a later table carries its own. The app role writes a request in the
-- upgrade's transaction, advances it page by page and deletes it when the
-- pass finishes the kind; erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.index_reprojections TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.index_reprojections TO substrate_maint', sch);
    END IF;
END $$;
