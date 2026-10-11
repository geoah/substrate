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
-- A row computed before this migration has the columns null, which a read
-- treats as unknown (span_end) or as sorting first (the record keys), and a
-- rebuild fills.
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS span_end timestamptz;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS first_record_id text;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS first_ord integer;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS last_record_id text;
ALTER TABLE observed_rollups ADD COLUMN IF NOT EXISTS last_ord integer;
