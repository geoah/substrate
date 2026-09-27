-- A record's entries, newest first: the record-scoped change feed and the
-- before-value walk (engine/changevalues.go) both read "this record's entries
-- under seq S, newest first, the first N". On (repository, record_id) alone
-- that is every entry the record ever had, sorted, per read; with seq as the
-- last column it is a backward range scan that stops after N. The two-column
-- index it replaces is a prefix of this one, so every reader of it is served
-- here and it is dropped rather than maintained twice on every append.
--
-- Plain, not CONCURRENTLY: the runner applies each migration inside a
-- transaction, where CONCURRENTLY is refused.
CREATE INDEX IF NOT EXISTS changelog_record_seq_idx ON changelog (repository, record_id, seq);
DROP INDEX IF EXISTS changelog_record_idx;
