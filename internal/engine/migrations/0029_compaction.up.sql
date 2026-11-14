-- The compaction pass (decision 0300) and what it reads.
--
-- changelog_foreign is an index of the history: one row for each line that
-- carries an effect on a record other than the one it is addressed to (a
-- merge's loser, a resync's scope, an effect a transaction settled onto its
-- last line). A compaction reads a record's history by its address
-- (changelog_record_idx) and by this, never by scanning the whole history.
-- The write path adds a line's rows with the line, a compaction rewrites them
-- with the lines it changes, and a repository whose history came in another
-- way (an upgrade, a restore, an import) has them derived again by the pass
-- before it compacts anything: compaction_state.foreign_upto says how far.
-- It is derived from the changelog alone, so `repository rebuild` leaves it.
CREATE TABLE IF NOT EXISTS changelog_foreign (
    repository  text   COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text   COLLATE "C" NOT NULL,
    record_id   text   COLLATE "C" NOT NULL,
    seq         bigint NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, seq)
);
CREATE INDEX IF NOT EXISTS changelog_foreign_seq ON changelog_foreign (repository, seq);

-- One row per repository: how far the foreign-effects index has been
-- derived, and, per kind the pass compacts, the seq it has compacted the
-- kind's records up to (compaction_marks).
-- horizon is the oldest seq a client may resume from: the newest line a
-- forgotten deletion was on (compactpass.go), and forgot_upto how far the
-- pass has looked for deletions to forget. pass_at is when the last pass
-- ended, and pass_ended whether it ended on its budget, leaving the rest to
-- the next. held_after, per kind, is where the retries of its held records
-- (compaction_held) got to: one batch a pass, in turn.
CREATE TABLE IF NOT EXISTS compaction_state (
    repository   text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    foreign_upto bigint      NOT NULL DEFAULT 0,
    foreign_done boolean     NOT NULL DEFAULT false,
    horizon      bigint      NOT NULL DEFAULT 0,
    forgot_upto  bigint      NOT NULL DEFAULT 0,
    pass_at      timestamptz,
    pass_ended   boolean     NOT NULL DEFAULT false,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

CREATE TABLE IF NOT EXISTS compaction_marks (
    repository text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    kind       text        COLLATE "C" NOT NULL,
    upto       bigint      NOT NULL DEFAULT 0,
    records    bigint      NOT NULL DEFAULT 0,
    removed    bigint      NOT NULL DEFAULT 0,
    held_after text        COLLATE "C" NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, kind)
);

-- The records a compaction left part of because a hold that ends kept it (a
-- line a parked failure or a drain names, the webhook spool): each pass tries
-- them again first, while its mark per kind moves on (compactpass.go).
CREATE TABLE IF NOT EXISTS compaction_held (
    repository  text COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text COLLATE "C" NOT NULL,
    record_id   text COLLATE "C" NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id)
);

-- The archive's pieces a compaction made stale: every seq it rewrote or
-- removed, with the compaction that touched it last (archive_records.rewrites
-- counts them). An archive pass writes again only the pieces holding one, and
-- clears the rows its snapshot saw (archive.go), so what it uploads is bounded
-- by what the compactions changed, not by the history's length. It replaces
-- the one lowest seq archive_records.rewrite_from held.
CREATE TABLE IF NOT EXISTS archive_rewritten (
    repository text   COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    seq        bigint NOT NULL,
    rewrite    bigint NOT NULL,
    PRIMARY KEY (repository, seq)
);
ALTER TABLE archive_records DROP COLUMN IF EXISTS rewrite_from;

-- Every line a reader found not to verify (damage.go): no compaction and no
-- erasure reaches it or above it until its row verifies again. Not derived
-- from the changelog, which holds the damage and not the finding, so a
-- rebuild leaves it.
CREATE TABLE IF NOT EXISTS changelog_damage (
    repository text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    seq        bigint      NOT NULL,
    noted_by   text        NOT NULL,
    noted_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, seq)
);

ALTER TABLE changelog_foreign ENABLE ROW LEVEL SECURITY;
ALTER TABLE changelog_foreign FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS changelog_foreign_repository ON changelog_foreign;
CREATE POLICY changelog_foreign_repository ON changelog_foreign FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE compaction_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_state FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_state_repository ON compaction_state;
CREATE POLICY compaction_state_repository ON compaction_state FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE archive_rewritten ENABLE ROW LEVEL SECURITY;
ALTER TABLE archive_rewritten FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS archive_rewritten_repository ON archive_rewritten;
CREATE POLICY archive_rewritten_repository ON archive_rewritten FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE compaction_held ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_held FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_held_repository ON compaction_held;
CREATE POLICY compaction_held_repository ON compaction_held FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE changelog_damage ENABLE ROW LEVEL SECURITY;
ALTER TABLE changelog_damage FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS changelog_damage_repository ON changelog_damage;
CREATE POLICY changelog_damage_repository ON changelog_damage FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE compaction_marks ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_marks FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_marks_repository ON compaction_marks;
CREATE POLICY compaction_marks_repository ON compaction_marks FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.changelog_foreign TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_state TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_marks TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_held TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_rewritten TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.changelog_damage TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.changelog_foreign TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_state TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_marks TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_held TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.archive_rewritten TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.changelog_damage TO substrate_maint', sch);
    END IF;
END $$;
