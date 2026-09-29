-- A TEST FIXTURE, never a migration. The runner embeds
-- internal/engine/migrations/ alone, so no database ever applies this file.
--
-- It is the shape docs/operations.md refuses: a migration that edits a fold
-- table and writes no changelog entry. migrationrefold_db_test.go applies it
-- where a migration would run and requires the refold comparison to FAIL on
-- `records`, which is what keeps that test from passing vacuously: a harness
-- that cannot see this edit sees nothing.
UPDATE records
SET labels = labels || '{"fixture/migrated": true}'::jsonb
WHERE deleted_at IS NULL;
