-- The refs index loses a column and gains the reverse read's sort order.
--
-- `created_at` GOES, because nothing durable defined it. No reader served it:
-- `incoming` orders by the index key and reports the SOURCE RECORD's creation,
-- and the cascade sweep stands on the target and reads nothing else. Worse, it
-- was not a function of the row's own durable state: a live re-projection
-- stamped a re-derived row with the apply's clock while a rebuild stamped the
-- same row with the replayed entry's, so one changelog under one closure folded
-- to two different tables and the byte-for-byte rebuild contract had a column it
-- could not hold. A derived projection with no timestamp is reproducible by
-- construction.
ALTER TABLE refs DROP COLUMN IF EXISTS created_at;

-- The reverse read's index covers its SORT, not only its match. `incoming`
-- matches on (repository, dst_kind, dst) and pages ORDER BY (src_kind, src,
-- property, path, ord); under the three-column index of 0010 a hot target sorted
-- its whole match set for every page, so a record with tens of thousands of
-- pointers paid the sort per page. The ordering key is appended to the match
-- key, in the order the query asks for it, which is what makes the page an
-- index-ordered scan with no sort node.
--
-- The old index is a prefix of this one, so it is dropped rather than kept: a
-- prefix index answers nothing the wider one does not, and it would be a second
-- copy of the same rows to write on every record write.
DROP INDEX IF EXISTS refs_dst_idx;
CREATE INDEX IF NOT EXISTS refs_dst_idx
    ON refs (repository, dst_kind, dst, src_kind, src, property, path, ord);
