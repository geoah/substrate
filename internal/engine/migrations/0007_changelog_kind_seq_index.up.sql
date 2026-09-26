-- The dispatcher's read (engine/functions.go changesPast) names the kinds a
-- record trigger can match and walks each in seq order past the trigger's
-- cursor: `kind = $n AND seq > $cursor ORDER BY seq LIMIT 200`, one branch
-- per kind under a UNION ALL. Before, the read
-- named no kind and walked every entry past the cursor, so a replay over one
-- small kind in a large repository read the whole changelog (#637). With seq
-- in the key, each named kind's range starts at the cursor instead of at the
-- kind's first entry. The distinct-kind skip scan (changelogKinds) probes
-- this index as well.
--
-- It replaces changelog_kind_idx: (repository, kind) is its prefix, so every
-- read that index served, this one serves.
CREATE INDEX IF NOT EXISTS changelog_kind_seq_idx ON changelog (repository, kind, seq);
DROP INDEX IF EXISTS changelog_kind_idx;
