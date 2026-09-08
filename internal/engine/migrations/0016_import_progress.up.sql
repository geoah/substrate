-- The import-progress marker: one row per repository whose boot import has
-- begun and whose fold has not been rebuilt from the imported entries yet
-- (repodir.go importEntries). The row is written before the first batch of
-- changelog rows commits and deleted in the transaction that commits the last
-- fold pass, so a crash anywhere between leaves the row behind and the next
-- boot resumes the import instead of serving the fold as it stands. While
-- the row exists no dataset opens on the repository (ErrImportIncomplete).
--
-- The changelog head alone cannot carry this: the rows land in bounded
-- batches and the fold runs after the last one, so a crash after that batch
-- leaves the file head equal to the table head with nothing folded, which
-- reads as a repository that is up to date and empty.
--
-- `file_head` is the head the import is bringing the table to, for the boot
-- log and the refusal's message; the resume itself reads the table head.
CREATE TABLE IF NOT EXISTS import_progress (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    file_head  bigint      NOT NULL CHECK (file_head >= 0),
    started_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- The same row level security every repository-scoped table gets (0001).
ALTER TABLE import_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_progress FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS import_progress_repository ON import_progress;
CREATE POLICY import_progress_repository ON import_progress FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- 0001 granted the tables that existed when it ran, so a table added later
-- carries its own grants. The import runs on the application pool: it writes
-- the row, re-writes it when a boot resumes, and deletes it when the fold
-- completes. Erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.import_progress TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.import_progress TO substrate_maint', sch);
    END IF;
END $$;
