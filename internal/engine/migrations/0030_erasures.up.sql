-- What remains to erase (decision 0304): one row per intent to erase a
-- record's history up to a line, from the `erase` effect a permanent delete or
-- a "forget history" writes on its own line. A FOLD TABLE: the fold fills it
-- from that effect, and the erasure that does the work takes the effect off
-- its line and the row out in the same transaction, so a rebuild and a
-- restore from the archive give the same rows. live says the record's
-- current state stays; line is the seq of the line that carries the effect,
-- and since that line's time. owned says everything the record owns goes
-- with it, in the pass's batches, acting for on_behalf_of, which find it by
-- the record's id and by former_ids, a JSON list of the ids it had besides,
-- kept here because its purge takes its former_ids rows.
CREATE TABLE IF NOT EXISTS compaction_erasures (
    repository  text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        COLLATE "C" NOT NULL,
    record_id   text        COLLATE "C" NOT NULL,
    upto        bigint      NOT NULL,
    live        boolean     NOT NULL,
    line        bigint      NOT NULL,
    since       timestamptz NOT NULL,
    owned       boolean     NOT NULL DEFAULT false,
    on_behalf_of text       COLLATE "C" NOT NULL DEFAULT '',
    former_ids  jsonb       NOT NULL DEFAULT '[]',
    PRIMARY KEY (repository, record_kind, record_id, line)
);

-- How many records each erasure of what a record owns has purged so far,
-- for the status: not a fold table, so a rebuild leaves it.
CREATE TABLE IF NOT EXISTS compaction_closures (
    repository  text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        COLLATE "C" NOT NULL,
    record_id   text        COLLATE "C" NOT NULL,
    purged      bigint      NOT NULL DEFAULT 0,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, record_kind, record_id)
);
ALTER TABLE compaction_closures ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_closures FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_closures_repository ON compaction_closures;
CREATE POLICY compaction_closures_repository ON compaction_closures FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- A stored Idempotency-Key outcome is a record's values too: an erasure
-- drops the outcome of every key that answered with the record, keeping the
-- row so its effect stays run-once, and says so to a retry.
ALTER TABLE idempotency_keys ADD COLUMN IF NOT EXISTS erased boolean NOT NULL DEFAULT false;

ALTER TABLE compaction_erasures ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_erasures FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_erasures_repository ON compaction_erasures;
CREATE POLICY compaction_erasures_repository ON compaction_erasures FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_erasures TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_closures TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_erasures TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_closures TO substrate_maint', sch);
    END IF;
END $$;
