-- The ambiguity mark: the moment a mapping source was last written and left
-- unlinked because its probe found several candidates under a mapping whose
-- `onAmbiguous` is `park` (engine/ambiguous.go). DERIVED STORAGE on the row,
-- like `orphaned_at`: the source's own write sets and clears it, the
-- changelog never carries it, and a rebuild derives it again from the fold it
-- replayed. It is NULL on every record that is not one.
ALTER TABLE records ADD COLUMN IF NOT EXISTS ambiguous_at timestamptz;

-- The list read: the live marked rows of one kind. Partial, because the mark
-- is null on all but a handful of rows.
CREATE INDEX IF NOT EXISTS records_ambiguous_at_idx
    ON records (repository, kind)
    WHERE deleted_at IS NULL AND ambiguous_at IS NOT NULL;
