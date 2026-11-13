-- A repository's archive is copied after its writes commit (decisions 0262
-- and 0279). These columns are the control plane's summary of the last
-- publication, which `repository list` reads without the store: the seq it
-- holds every line up to, when, to which store, and how many blobs the
-- records at that seq hold that it does not. `repository rebuild` leaves
-- them alone.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS archived_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS archived_at timestamptz;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS archive_store text;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS archive_missing integer NOT NULL DEFAULT 0;
-- Set when the repository's deletion begins (decision 0279, rule 7): no
-- archive pass runs on it from then on, and a boot finishes the deletion.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS deletion_began_at timestamptz;

-- THE RECORD (decision 0279): what the database keeps of its repository's
-- archive. One row per repository: the archive it writes, the highest epoch
-- it has drawn there, the manifest it last published (its name and bytes),
-- the exact bytes of the one it is about to publish, why the last pass
-- stopped, and the manifest a restore reads from. It is history's copy's own
-- record: no changelog entry
-- writes it, so it is not a fold table, and `repository rebuild` leaves it.
--
-- Every key column sorts in byte order (0021), the repository's id, an
-- archive's id and an object's name alike.
CREATE TABLE IF NOT EXISTS archive_records (
    repository      text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    archive         text        COLLATE "C" NOT NULL,
    epoch           bigint      NOT NULL DEFAULT 0,
    published       text,
    published_bytes bytea,
    pending         bytea,
    stopped         text,
    source          text,
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- One row for every object the database has put into its archive or is
-- deleting from it, written before the request is sent: its name under the
-- archive, what it holds (a blob's digest, a sealed value's ref and version,
-- a spool file, a piece's seqs), the process that put it, where it stands
-- (putting, put, dropped, deleting), and whether the published manifest and
-- the one about to be published select it.
CREATE TABLE IF NOT EXISTS archive_objects (
    repository   text    COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    archive      text    COLLATE "C" NOT NULL,
    name         text    COLLATE "C" NOT NULL,
    holds        text    NOT NULL,
    process      text    NOT NULL,
    state        text    NOT NULL,
    in_published boolean NOT NULL DEFAULT false,
    in_pending   boolean NOT NULL DEFAULT false,
    PRIMARY KEY (repository, archive, name)
);

-- THE OPERATOR'S CHOICE TO TAKE THE ARCHIVE (rule 4): one row per
-- repository whose replacement by its archive is chosen and not finished,
-- naming the copy staged for it, <data root>/archive-take/<repository
-- id>/<stage>/, and the archive and the manifest that copy holds, the
-- manifest's name carrying the hash of its bytes. Only a row starts or
-- continues a replacement, and only with the copy it names; a staged copy
-- no row names is removed and never installed. The row is the control
-- plane's, keyed like the repository's own row and not by a `repository`
-- column, because it outlives every repository-scoped row: the boot acts on
-- it between their removal and the end of the restore. The restore's last
-- transaction deletes it, as does the transaction that begins a deletion.
CREATE TABLE IF NOT EXISTS archive_takes (
    repository_id text        COLLATE "C" PRIMARY KEY,
    stage         text        COLLATE "C" NOT NULL,
    archive       text        COLLATE "C" NOT NULL,
    manifest      text        COLLATE "C" NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now()
);

-- The restore's last transaction runs on the repository's own connection,
-- which reaches the repository's row here and no other.
ALTER TABLE archive_takes ENABLE ROW LEVEL SECURITY;
ALTER TABLE archive_takes FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS archive_takes_repository ON archive_takes;
CREATE POLICY archive_takes_repository ON archive_takes FOR ALL
    USING (repository_id = current_setting('substrate.repository', true))
    WITH CHECK (repository_id = current_setting('substrate.repository', true));

-- Repository-scoped like every table a dataset writes, with 0011's row level
-- security: the `repository` column defaults to the connection's setting and
-- the policy holds each row to it.
ALTER TABLE archive_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE archive_records FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS archive_records_repository ON archive_records;
CREATE POLICY archive_records_repository ON archive_records FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE archive_objects ENABLE ROW LEVEL SECURITY;
ALTER TABLE archive_objects FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS archive_objects_repository ON archive_objects;
CREATE POLICY archive_objects_repository ON archive_objects FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered what existed then, so a later
-- table carries its own. The pass writes through the app role; a restore, a
-- deletion and the operator's commands run on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_records TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_objects TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, DELETE ON TABLE %I.archive_takes TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_records TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_objects TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_takes TO substrate_maint', sch);
    END IF;
END $$;
