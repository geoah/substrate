-- The catch-up for the superseded 0005 (see supersededSHA256 in migrate.go).
-- 0005 gained repositories_signed_from_positive after some databases had
-- already applied it, so this adds the constraint wherever it is missing and
-- leaves every database that ran the landed 0005 alone.
--
-- Conditional rather than IF NOT EXISTS: ALTER TABLE ... ADD CONSTRAINT has
-- no such form.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'repositories'::regclass
          AND conname = 'repositories_signed_from_positive'
    ) THEN
        ALTER TABLE repositories
            ADD CONSTRAINT repositories_signed_from_positive
                CHECK (signed_from_seq IS NULL OR signed_from_seq >= 1);
    END IF;
END
$$;
