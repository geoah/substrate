-- What a rollup row keeps beyond 0012's columns, so a read served from the
-- rows answers what the raw points answer (internal/observed, the second
-- aggregate review).
--
-- `span_end` is the latest end of an amount row's spans. The row's
-- `covered_ms` is the union of its spans, all inside [first_ts, span_end),
-- so a read joining two rows' coverage adds their lengths where those
-- extents do not overlap, and takes the union from the raw spans where they
-- do. Null for the other forms.
--
-- `first_record_id`, `first_ord`, `last_record_id` and `last_ord` are the
-- record id and index of the row's first and last point. Points at one
-- instant are ordered by record id and index, so two rows whose first (or
-- last) points share an instant are ordered as the raw points are, and a
-- recorded-local day's first and last offsets come from real points.
--
-- `min_value` and `max_value` are the stored digits of the row's smallest and
-- largest number, compared exactly, beside the float8 `min` and `max`: a
-- float8 holds 15 to 17 digits, and a min or max answers the digits stored.
--
-- A row computed before this migration has the columns null, which a read
-- treats as unknown (span_end), as sorting first (the record keys) or as
-- inexact (the float8 extremes), and a rebuild fills.
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS span_end timestamptz;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS first_record_id text;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS first_ord integer;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS last_record_id text;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS last_ord integer;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS min_value numeric;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS max_value numeric;
