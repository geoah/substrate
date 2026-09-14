-- The window read's series candidates: every live row whose properties carry
-- a recurrence rule or a NON-EMPTY list of extra dates (the core `recurring`
-- trait's two ways to name occurrences; an empty `rdates` is an ordinary
-- row). A partial index rather than a column, so the fold stays untouched:
-- the predicate is over `props` alone and the read spells it the same way,
-- beside `kind IN <the kinds that bind the trait>`. `?` (key exists) is not
-- served by the jsonb_path_ops GIN index records already carries, which is
-- why this one exists.
CREATE INDEX IF NOT EXISTS records_recurring_idx
    ON records (repository, kind, id)
    WHERE deleted_at IS NULL
      AND (props ? 'recurrence'
           OR (jsonb_typeof(COALESCE(props->'rdates', 'null'::jsonb)) = 'array' AND props->'rdates' <> '[]'::jsonb));
