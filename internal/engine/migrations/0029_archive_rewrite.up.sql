-- A compaction (decision 0296) rewrites lines an archive's pieces already
-- hold: the state entries it writes, the lines it removes and the
-- transactions it frames again. It records the lowest seq it changed, and
-- the archive pass writes its pieces again from the one holding it
-- (archive.go). `rewrites` counts the compactions, so a pass clears the mark
-- only when no compaction came after the snapshot it read the mark in.
ALTER TABLE archive_records ADD COLUMN IF NOT EXISTS rewrite_from bigint;
ALTER TABLE archive_records ADD COLUMN IF NOT EXISTS rewrites bigint NOT NULL DEFAULT 0;
