-- A person's sign-in is an IDENTITY the server holds, beside the
-- repositories, not a record of one repository (decision 0274). Like
-- `repositories`, both tables are the control plane's: the maintenance pool
-- reads and writes them, and the repository-scoped role never sees them.
--
-- `identities` holds the name a person signs in with and the factors, sealed
-- under a data key of the identity's own, which is wrapped under the host
-- credential key and bound to the id, as a repository's is (dek.go). Nothing
-- here is a fold table and no row is written: the boot gives each repository
-- a release before identities left with a credential record its identity
-- (engine/identities.go), from that record and its sealed material.
CREATE TABLE IF NOT EXISTS identities (
    id         text        COLLATE "C" PRIMARY KEY,
    name       text        NOT NULL,
    dek        bytea       NOT NULL,
    dek_key_id text,
    password   bytea       NOT NULL,
    totp       bytea,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS identities_name_key ON identities (name);

-- Which repositories an identity may open. One row per pair, so one person
-- may be in several repositories and one repository may hold several people;
-- 1.0 writes one per repository. The repository's id is `repository_id`, not
-- `repository`: that column name marks a table under row level security
-- (repositoryScopedTables), and this one is the control plane's. It carries
-- no foreign key: a repository's row is moved, not updated, when the boot
-- keys it (repositoryid.go), and erasing a repository deletes its rows here
-- itself.
CREATE TABLE IF NOT EXISTS identity_repositories (
    identity      text        COLLATE "C" NOT NULL REFERENCES identities (id) ON DELETE CASCADE,
    repository_id text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (identity, repository_id)
);
CREATE INDEX IF NOT EXISTS identity_repositories_repository_idx ON identity_repositories (repository_id);

DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('REVOKE ALL ON TABLE %I.identities FROM substrate_app', sch);
        EXECUTE format('REVOKE ALL ON TABLE %I.identity_repositories FROM substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.identities TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.identity_repositories TO substrate_maint', sch);
    END IF;
END $$;
