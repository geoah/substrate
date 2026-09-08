-- Every changelog row names the transaction that appended it: `txn` is the
-- seq of that transaction's last entry, stamped at commit beside `hash`
-- (checksum.go settleChecksums) and carried by the segment line as `txn`
-- (changelogfile.Entry.Txn, line format 2), so the boot's table-to-file
-- catch-up appends whole transactions and never leaves a prefix of one in a
-- segment (repodir.go appendFromTable). NULL on every row a binary before
-- this migration wrote (v0.46.0, v0.47.0): those rows recorded no boundary
-- and none is invented for them; each is written out as a transaction of its
-- own, which is how its line already read. NOT VALID, as 0005 and 0015 did
-- for this table: every existing row is NULL and passes, and validating them
-- would scan the whole changelog under an ACCESS EXCLUSIVE lock at the first
-- boot for nothing. Idempotent: a database that has the column and the
-- constraint passes through.
ALTER TABLE changelog ADD COLUMN IF NOT EXISTS txn bigint;

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'changelog_txn_ends_at_or_after_seq'
          AND conrelid = 'changelog'::regclass
    ) THEN
        ALTER TABLE changelog
            ADD CONSTRAINT changelog_txn_ends_at_or_after_seq CHECK (txn IS NULL OR txn >= seq) NOT VALID;
    END IF;
END $$;
