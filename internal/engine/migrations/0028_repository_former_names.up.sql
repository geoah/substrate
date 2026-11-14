-- A repository's former names (decision record 0287). A rename moves the
-- repository's name in `repositories.authority` and keeps the name it had
-- here, with the time it stopped being the name: for 90 days from then the
-- name still resolves to the repository and no registration or rename
-- elsewhere takes it, and after that the row is spent and the next claim of
-- the name overwrites it. Deleting the repository takes its former names with
-- it, so they are free at once, as its name is (decision record 0236).
--
-- Control plane, as `repositories` is: no repository's role reads it, and
-- nothing in a repository's history or directory holds it.
CREATE TABLE IF NOT EXISTS repository_former_names (
    name          text        COLLATE "C" PRIMARY KEY,
    repository_id text        COLLATE "C" NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    retired_at    timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS repository_former_names_repository ON repository_former_names (repository_id);

DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.repository_former_names TO substrate_maint', sch);
    END IF;
END $$;
