-- The changelog dialect: one row per repository, a monotonic integer naming
-- the spelling its changelog entries are written in: the ops and fold
-- effects a binary must understand to replay them. It is the second half of
-- the gate `vocabulary_dialect` (0001) opened: that stamp governs stored
-- DECLARATION rows, this one governs HISTORY, so a binary that cannot replay
-- a repository's changelog refuses the open instead of serving it until
-- somebody runs a rebuild and discovers the entry it cannot fold.
--
-- The row is written by the transaction that appends the entries it claims,
-- never by an open on its own: a claim over history a binary has not written
-- would bar an older binary for nothing.
--
-- There is no promotions ledger beside it, because there are no promotions.
-- The vocabulary ladder rewrites stored rows step by step and records each
-- step; a changelog is append-only and its old entries keep their old
-- spelling forever, so this stamp only ever states what a replayer must
-- understand, and nothing ever runs to change what history says.
CREATE TABLE IF NOT EXISTS changelog_dialect (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    dialect    integer     NOT NULL CHECK (dialect >= 1),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- The same row level security every repository-scoped table gets (0001).
ALTER TABLE changelog_dialect ENABLE ROW LEVEL SECURITY;
ALTER TABLE changelog_dialect FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS changelog_dialect_repository ON changelog_dialect;
CREATE POLICY changelog_dialect_repository ON changelog_dialect FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- 0001 granted the tables that existed when it ran, so a table added later
-- carries its own grants. The gate runs at open on the application pool and
-- stamps through an upsert, so the app role needs UPDATE beside INSERT;
-- erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE ON TABLE %I.changelog_dialect TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.changelog_dialect TO substrate_maint', sch);
    END IF;
END $$;
