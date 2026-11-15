-- Where an erasure that goes on in steps stands (decision 0330): one row per
-- record whose erasure has taken a step. searched is the highest seq of the
-- history its search for the saved positions that hold the record's values
-- has read: a search reads the trigger lines in seq ranges held to the
-- compaction pass's bounds, one range a transaction, so a long trigger
-- history is never read whole under one lock, and the history does not
-- carry how far a search has looked. upto is the line of the intent that
-- search was made for: a wider intent (the record deleted again before the
-- erasure finished) erases lines above it, which a trigger line the search
-- passed may hold a position about, so a search for another upto starts
-- again from the record's first line. updated_at is when the erasure last
-- took a step: a pass takes the least recently stepped first. held is why its last step failed on
-- its own, a step refused or a line that does not verify, so the pass goes
-- on to the next erasure and an operator reads why; a step that goes on
-- clears it. NOT A FOLD TABLE: it is progress, beside compaction_erasures as
-- compaction_held is beside compaction_marks. A rebuild keeps it, since
-- the lines it read are unchanged; a restore and an import start without
-- it, and the search starts again from the start, which is safe because a
-- saved position it stripped no longer matches. The erasure that finishes
-- takes the row out with the intent.
CREATE TABLE IF NOT EXISTS compaction_erasure_progress (
    repository  text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        COLLATE "C" NOT NULL,
    record_id   text        COLLATE "C" NOT NULL,
    searched    bigint      NOT NULL DEFAULT 0,
    upto        bigint      NOT NULL DEFAULT 0,
    held        text        NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, record_kind, record_id)
);

ALTER TABLE compaction_erasure_progress ENABLE ROW LEVEL SECURITY;
ALTER TABLE compaction_erasure_progress FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS compaction_erasure_progress_repository ON compaction_erasure_progress;
CREATE POLICY compaction_erasure_progress_repository ON compaction_erasure_progress FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_erasure_progress TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.compaction_erasure_progress TO substrate_maint', sch);
    END IF;
END $$;
