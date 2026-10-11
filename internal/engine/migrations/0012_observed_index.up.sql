-- The observed index (decision 0160, engine/observedindex.go): three tables
-- the fold derives from the rows of every kind that binds core's `observed`
-- trait, the way it derives `refs`. Each row is a function of one record's
-- properties and its kind's binding, or of the points those produce, and of
-- nothing else (no clock, no other record), so `repository rebuild` clears
-- the three and reproduces them from the changelog. `repository verify` and
-- the export never read them.
--
-- A reference column (`metric`, `source`, `about`) holds the target's record
-- path, `<kind>/<id>`. An absent `source`, `about`, `channel`, `code` or
-- `unit` is the empty string and an absent UTC offset is 0, so the stream
-- columns can key a row.
--
-- observed_points: one row per value, one per observation and one per point
-- of a trace, null points of a trace included (`value` null, `code` empty,
-- `quality` 'unavailable'), because a null ends the hold of the point before
-- it. A record's rows are deleted and inserted whole on every write of it and
-- deleted by its tombstone. `ord` is the point's index in its record.
-- `value_exact` is the stored digits of every number and `value_base` the
-- number in its measure's base unit (in `temperature-difference` where
-- `delta`), null where the unit has no base (arbitrary units, levels, rad).
-- The hold, the counter contract and the circular flag ride on every point,
-- because a stream spans records and the rollups are computed from the
-- points alone.
CREATE TABLE IF NOT EXISTS observed_points (
    repository     text        NOT NULL DEFAULT current_setting('substrate.repository'),
    kind           text        NOT NULL,
    record_id      text        NOT NULL,
    ord            integer     NOT NULL,
    metric         text        NOT NULL,
    source         text        NOT NULL DEFAULT '',
    about          text        NOT NULL DEFAULT '',
    channel        text        NOT NULL DEFAULT '',
    form           text        NOT NULL,
    ts             timestamptz NOT NULL,
    ts_end         timestamptz NOT NULL,
    utc_offset_min integer     NOT NULL DEFAULT 0,
    unit           text        NOT NULL DEFAULT '',
    delta          boolean     NOT NULL DEFAULT false,
    value          float8,
    value_exact    numeric,
    value_base     float8,
    code           text        NOT NULL DEFAULT '',
    statistic      text,
    bounded        boolean     NOT NULL DEFAULT false,
    quality        text,
    hold_ms        bigint,
    reset_rule     text,
    reset_at       timestamptz,
    wrap_at        numeric,
    circular       boolean     NOT NULL DEFAULT false,
    revision       bigint,
    PRIMARY KEY (repository, kind, record_id, ord),
    CHECK ((value IS NULL) = (value_exact IS NULL))
);
-- A stream's points in time order: a range read, the rollup recompute of a
-- bucket, the point current at a window's start and the latest point, each
-- one index range.
CREATE INDEX IF NOT EXISTS observed_points_stream_idx
    ON observed_points (repository, metric, kind, source, about, channel, ts);

-- observed_rollups: one row per stream, group and 15-minute bucket (UTC
-- aligned, which every offset in use divides), for gauges, amounts and
-- counters and never for summaries, computed by internal/observed.Bucket
-- from the bucket's points. A group is a code, a unit, the delta flag and a
-- UTC offset: the statistics are in the row's own `unit`, so two units that
-- share no base never add. Every bucket the old and the new points of a
-- transaction touch is recomputed once at its end, and a counter's next
-- bucket with a point too, because its first step comes from the previous
-- point. The key puts the bucket before the group, so a recompute deletes
-- and a read ranges over one stream's buckets on the key.
--
-- A read served from the rows answers what the raw points answer
-- (internal/observed), so a row keeps three things beyond its statistics,
-- and the CHECKs hold every row to them:
--
--   - `min_value` and `max_value`, the stored digits of the row's smallest
--     and largest number, compared exactly, beside the float8 `min` and
--     `max`: a float8 holds 15 to 17 digits, and a min or max answers the
--     digits stored.
--   - `first_record_id`, `first_ord`, `last_record_id` and `last_ord`, the
--     record id and index of the first and last point. Points at one instant
--     are ordered by record id and index, so two rows whose first (or last)
--     points share an instant are ordered as the raw points are.
--   - `span_end`, the latest end of an amount row's spans. Its `covered_ms`
--     is the union of the spans, all inside [first_ts, span_end), so a read
--     joining two rows adds their lengths where those extents do not
--     overlap and takes the union from the raw spans where they do.
--
-- `increase` is null where the row holds no step (a counter's reading with
-- none before it): no answer, not a change of zero.
CREATE TABLE IF NOT EXISTS observed_rollups (
    repository     text        NOT NULL DEFAULT current_setting('substrate.repository'),
    metric         text        NOT NULL,
    kind           text        NOT NULL,
    source         text        NOT NULL,
    about          text        NOT NULL,
    channel        text        NOT NULL,
    form           text        NOT NULL,
    code           text        NOT NULL,
    unit           text        NOT NULL,
    delta          boolean     NOT NULL,
    utc_offset_min integer     NOT NULL,
    bucket15       timestamptz NOT NULL,
    n              bigint      NOT NULL,
    bounded        bigint      NOT NULL,
    sum            float8,
    min            float8,
    max            float8,
    min_value      numeric,
    max_value      numeric,
    mean           float8,
    m2             float8,
    first_ts       timestamptz,
    first_value    numeric,
    first_record_id text,
    first_ord      integer,
    last_ts        timestamptz,
    last_value     numeric,
    last_record_id text,
    last_ord       integer,
    covered_ms     bigint      NOT NULL,
    span_end       timestamptz,
    held_ms        bigint,
    twa_sum        float8,
    last_hold_ms   bigint,
    max_gap_ms     bigint      NOT NULL,
    start_gap_ms   bigint      NOT NULL,
    increase       numeric,
    resets         integer,
    sin_sum        float8,
    cos_sum        float8,
    PRIMARY KEY (repository, metric, kind, source, about, channel, form, bucket15, code, unit, delta, utc_offset_min),
    CHECK ((min IS NULL) = (min_value IS NULL) AND (max IS NULL) = (max_value IS NULL)),
    CHECK ((first_ts IS NULL) = (first_record_id IS NULL) AND (first_ts IS NULL) = (first_ord IS NULL)),
    CHECK ((last_ts IS NULL) = (last_record_id IS NULL) AND (last_ts IS NULL) = (last_ord IS NULL)),
    CHECK ((span_end IS NOT NULL) = (form = 'amount' AND n > 0)),
    CHECK (increase IS NULL OR form = 'counter')
);

-- observed_latest: one row per stream, its newest available point, so the
-- stream list and the current value of each read one row. Ties at one
-- instant go to the higher revision, then the greater record id, then the
-- later point. `points` counts the stream's available points.
CREATE TABLE IF NOT EXISTS observed_latest (
    repository     text        NOT NULL DEFAULT current_setting('substrate.repository'),
    metric         text        NOT NULL,
    kind           text        NOT NULL,
    source         text        NOT NULL,
    about          text        NOT NULL,
    channel        text        NOT NULL,
    ts             timestamptz NOT NULL,
    ts_end         timestamptz NOT NULL,
    value          numeric,
    code           text        NOT NULL,
    unit           text        NOT NULL,
    delta          boolean     NOT NULL,
    form           text        NOT NULL,
    statistic      text,
    bounded        boolean     NOT NULL,
    hold_ms        bigint,
    utc_offset_min integer     NOT NULL,
    record_id      text        NOT NULL,
    revision       bigint,
    points         bigint      NOT NULL,
    PRIMARY KEY (repository, metric, kind, source, about, channel)
);

-- Repository-scoped like every table a dataset writes, with 0011's row
-- level security: the `repository` column defaults to the connection's
-- setting and the policy holds each row to it.
ALTER TABLE observed_points ENABLE ROW LEVEL SECURITY;
ALTER TABLE observed_points FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS observed_points_repository ON observed_points;
CREATE POLICY observed_points_repository ON observed_points FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE observed_rollups ENABLE ROW LEVEL SECURITY;
ALTER TABLE observed_rollups FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS observed_rollups_repository ON observed_rollups;
CREATE POLICY observed_rollups_repository ON observed_rollups FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE observed_latest ENABLE ROW LEVEL SECURITY;
ALTER TABLE observed_latest FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS observed_latest_repository ON observed_latest;
CREATE POLICY observed_latest_repository ON observed_latest FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered the tables that existed then,
-- so a later table carries its own. The app role writes all three in the
-- write's transaction; erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_points TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_points TO substrate_maint', sch);
    END IF;
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_rollups TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_rollups TO substrate_maint', sch);
    END IF;
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_latest TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.observed_latest TO substrate_maint', sch);
    END IF;
END $$;

-- The background re-derivation (engine/reprojection.go) gains the observed
-- index as its third: a kind that starts or stops binding `observed`, or
-- whose rows predate this table, has every row's points re-derived behind
-- the open or the apply that asked for it.
ALTER TABLE index_reprojections ADD COLUMN IF NOT EXISTS observed boolean NOT NULL DEFAULT false;
