-- Each observed segment's earliest available point (engine/observedstore.go
-- segment). The latest read answers a group's earliest point (`first`) from
-- it, and observed_segments.first_ts cannot: it is the record's first point,
-- available or null. observedIndexVersion 3 re-derives every row at the
-- repository's next open, which fills the column; until then it is null and
-- the aggregate reads over the index wait for the re-derivation.
ALTER TABLE observed_segments ADD COLUMN IF NOT EXISTS first_available timestamptz;

-- A stream's segments holding an available point, by that point: the
-- earliest is one index probe, whatever run of segments of nulls precedes it.
CREATE INDEX IF NOT EXISTS observed_segments_first_available_idx
    ON observed_segments (repository, stream, first_available)
    WHERE first_available IS NOT NULL;

-- Every segment stored before this column holds it null, so none of them is
-- derived under observedIndexVersion 3, whatever version observed_index
-- records: a store whose rows reached this schema by copy or restore from one
-- that already ran a version-3 binary would otherwise skip the re-derivation
-- and keep the nulls. Recording version 2 makes the next open re-derive.
UPDATE observed_index SET version = 2 WHERE version > 2;
