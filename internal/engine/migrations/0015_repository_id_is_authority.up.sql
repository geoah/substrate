-- The repository's id IS its authority (decision record 0046, amended): the
-- primary key, every scope, the DEK wrap's binding and the directory under the
-- data root are the one DNS-style name registration chose. The `authority`
-- column stays, because a landed migration is never edited and 0013 created
-- it, and this constraint holds it equal to `id` on every row written from
-- here on. No trigger fills one from the other: the engine writes both.
--
-- NOT VALID, so a database whose rows still carry the random ids the engine
-- used to mint applies the migration and then meets the boot check, which
-- refuses such a row and says to wipe the database and boot again (the
-- repository directory imports). The tree assumes fresh repositories before
-- v1; there is no data migration for the old rows.
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'repositories_id_is_authority'
          AND conrelid = 'repositories'::regclass
    ) THEN
        ALTER TABLE repositories
            ADD CONSTRAINT repositories_id_is_authority CHECK (id = authority) NOT VALID;
    END IF;
END $$;
