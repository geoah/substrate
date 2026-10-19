-- The semantic arm's candidates (decision 0174, engine/search.go
-- semanticCandidates): an HNSW index over a half-precision copy of every
-- stored vector, so a search reads the few hundred nearest chunks from the
-- graph instead of scoring every vector the resolved pair has. 0001 shipped
-- the table without one on purpose, while a repository held a few thousand
-- chunks; a full scan of a few hundred thousand 1536-wide vectors is seconds
-- to minutes a query on a small host.
--
-- Half precision halves the index, about 3 KB a chunk against 6 KB, so it has
-- a chance of staying in memory where `shared_buffers` is small. The stored
-- vectors stay full precision: the candidates are rescored exactly against
-- them, so half precision decides which chunks are looked at and never the
-- score a hit carries. The reader spells the same expression,
-- `vec::halfvec(1536)` under `<=>`, or the planner cannot match the index.
--
-- One index over every repository's rows, as every index here is: the
-- repository, the pair and the kinds are filters the scan applies as it walks
-- the graph (`hnsw.iterative_scan`, set by the reader), not columns of the
-- index.
--
-- The runner applies a migration inside a transaction, so this cannot build
-- CONCURRENTLY, and it holds writes to `embeddings` while it builds. On an
-- empty table the build is instant. An operator with a large table can build
-- the same index by name before upgrading, outside any transaction and
-- without blocking writes, and this statement is then a no-op (the type is
-- qualified because the extension lives in `public`, as 0001 qualifies the
-- column):
--
--   CREATE INDEX CONCURRENTLY IF NOT EXISTS embeddings_vec_hnsw_idx
--       ON embeddings USING hnsw ((vec::public.halfvec(1536)) public.halfvec_cosine_ops);
--
-- IF NOT EXISTS alone would accept any relation of the name: a CONCURRENTLY
-- build that failed leaves an invalid index, and one built on the
-- full-precision column, on another expression or operator class, on part of
-- the table or on another table has the name too. Each would make this
-- statement a no-op the reader can never use. So the name is looked up beside
-- the table it must index (never through the search path, which can reach
-- another schema's relation of that name), and unless it is a valid,
-- unconditional hnsw index on `embeddings` over `vec::halfvec(1536)` with
-- halfvec_cosine_ops, it is dropped and built again.
DO $$
DECLARE
    tbl regclass := to_regclass('embeddings');
    idx oid;
    kind "char";
BEGIN
    SELECT c.oid, c.relkind INTO idx, kind
      FROM pg_class c
     WHERE c.relname = 'embeddings_vec_hnsw_idx'
       AND c.relnamespace = (SELECT relnamespace FROM pg_class WHERE oid = tbl);
    -- A relation of the name that is not an index is somebody's data: the
    -- migration refuses to touch it and says what to do.
    IF idx IS NOT NULL AND kind <> 'i' THEN
        RAISE EXCEPTION 'embeddings_vec_hnsw_idx names a relation of kind % beside the embeddings table, not an index; rename or drop it, then start substrate again', kind;
    END IF;
    IF idx IS NOT NULL AND NOT EXISTS (
        SELECT 1
          FROM pg_index i
          JOIN pg_class c ON c.oid = i.indexrelid
          JOIN pg_am a ON a.oid = c.relam
          JOIN pg_opclass o ON o.oid = i.indclass[0]
         WHERE i.indexrelid = idx
           AND i.indrelid = tbl
           AND i.indisvalid
           AND i.indnatts = 1
           AND i.indpred IS NULL
           AND a.amname = 'hnsw'
           AND o.opcname = 'halfvec_cosine_ops'
           AND regexp_replace(pg_get_expr(i.indexprs, i.indrelid), '[()[:space:]]|public[.]', '', 'g') = 'vec::halfvec1536'
    ) THEN
        EXECUTE format('DROP INDEX %s', idx::regclass);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS embeddings_vec_hnsw_idx
    ON embeddings USING hnsw ((vec::public.halfvec(1536)) public.halfvec_cosine_ops);
