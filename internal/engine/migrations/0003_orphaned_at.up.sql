-- The orphan mark: the moment a mapping's target was last found with no live
-- source of its own, and nothing but machinery holding its properties
-- (engine/orphans.go). DERIVED STORAGE on the row, like `fts` and like
-- property_offers: it is a function of the live records, recompute sets and
-- clears it, the changelog never carries it, and a rebuild derives it again
-- from the fold it replayed. It is NULL on every record that is not one.
--
-- A column rather than a table because the mark is per RECORD, it is asked
-- for as a list predicate (`filter.orphaned`), and a sweep pages it by age.
ALTER TABLE records ADD COLUMN IF NOT EXISTS orphaned_at timestamptz;

-- The sweep's read: the live marked rows, oldest mark first. Partial, because
-- the mark is null on all but a handful of rows and the collection pass asks
-- for exactly this set.
CREATE INDEX IF NOT EXISTS records_orphaned_at_idx
    ON records (repository, orphaned_at)
    WHERE deleted_at IS NULL AND orphaned_at IS NOT NULL;
