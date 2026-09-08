-- A parked failure's id is the seq of the changelog entry that parked it
-- (engine delivery.go, decision 0064), so a repository directory imported
-- into an empty database folds its failures back under the ids the retry API
-- already handed out. A seq is unique per repository, not per database, so
-- the key moves from `id` alone to (repository, id) and the serial default
-- goes: nothing allocates ids from a sequence any more.
--
-- Rows a bigserial numbered before this migration keep their identity
-- negated. Those ids came from a database-wide sequence, so one could equal a
-- seq this repository has not reached yet, and a later park landing on that
-- seq would collide with it; below zero no seq can. They are not in the
-- changelog, so a rebuild or a restore does not bring them back, which the
-- release notes and docs/operations.md say.
--
-- Idempotent: a database whose key is already (repository, id) passes
-- through, and the negation runs only in the same branch as the key change,
-- so it cannot run twice.
DO $$
DECLARE
    pk text;
BEGIN
    SELECT pg_get_constraintdef(oid) INTO pk
    FROM pg_constraint
    WHERE conrelid = 'trigger_failures'::regclass AND contype = 'p';
    IF pk IS DISTINCT FROM 'PRIMARY KEY (repository, id)' THEN
        UPDATE trigger_failures SET id = -id WHERE id > 0;
        ALTER TABLE trigger_failures DROP CONSTRAINT IF EXISTS trigger_failures_pkey;
        ALTER TABLE trigger_failures ADD CONSTRAINT trigger_failures_pkey PRIMARY KEY (repository, id);
    END IF;
END $$;

ALTER TABLE trigger_failures ALTER COLUMN id DROP DEFAULT;
DROP SEQUENCE IF EXISTS trigger_failures_id_seq;
