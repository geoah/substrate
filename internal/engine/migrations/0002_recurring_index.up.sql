-- The window read's series candidates: every live row whose properties carry
-- a recurrence rule or extra dates (the core `recurring` trait's two ways to
-- name occurrences). A partial index rather than a column, so the fold stays
-- untouched: the predicate is over `props` alone and the read spells it the
-- same way. `?` (key exists) is not served by the jsonb_path_ops GIN index
-- records already carries, which is why this one exists.
CREATE INDEX IF NOT EXISTS records_recurring_idx
    ON records (repository, kind, id)
    WHERE deleted_at IS NULL AND (props ? 'recurrence' OR props ? 'rdates');
