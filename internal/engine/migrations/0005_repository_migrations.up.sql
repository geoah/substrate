-- The ledger of REPOSITORY MIGRATIONS (engine/repomigrate.go, decision record
-- 0099): code a binary runs once per repository, at the repository's first
-- open under a binary that carries it, recorded here the way
-- schema_migrations records the SQL the runner applied to the database. One
-- row per (repository, version); the name is the migration's own, so an
-- operator reading the ledger finds it in the tree by that word.
--
-- Repository-scoped like every table a dataset writes (repositories.go
-- repositoryScopedTables): the `repository` column defaults to the
-- connection's setting and row level security holds each row to it.
CREATE TABLE IF NOT EXISTS repository_migrations (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    version    integer     NOT NULL,
    name       text        NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, version)
);
ALTER TABLE repository_migrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE repository_migrations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS repository_migrations_repository ON repository_migrations;
CREATE POLICY repository_migrations_repository ON repository_migrations FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered the tables that existed then,
-- so a later table carries its own. The ledger is append-only for the app
-- role: a row is never updated, and erasing a repository runs on the maint
-- pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT ON TABLE %I.repository_migrations TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.repository_migrations TO substrate_maint', sch);
    END IF;
END $$;
