-- The live records of the given kinds a tsquery matches in the session's
-- repository, read through records_fts_idx, at most lim of them.
--
-- A lexical search counts a pass's matches through it, capped, and reads them
-- through it while there are few; a strict pass that runs out of its share of
-- the budget reading a common word's matches newest first takes some matches
-- from it instead (internal/engine/bm25.go, lexicalCandidates). Reading a
-- row's text vector is the whole cost of a common word: one row of an agent
-- transcript carries hundreds of kB of vector in TOAST, and on a repository
-- of 620k records the 697 matches of `dinner` took 9.4 s to rank cold.
--
-- records_matching (migration 0010) is not enough for either job. Its cap
-- reaches inside before any kind filter, so a search naming kinds that are a
-- small share of the matches got back few of them or none; this function
-- applies the kinds inside, before the cap. And its body is planned
-- generically, LIMIT unknown, and the planner prices a sequential scan
-- without the TOAST it detoasts: counting `dinner` there scanned the whole
-- table and read every row's vector, some 900 MiB of TOAST. This function is
-- planned with sequential and plain index scans off, which leaves the bitmap
-- scans, and of those the one through records_fts_idx answers the match from
-- the index without reading a vector.
--
-- It is SECURITY DEFINER and owned by substrate_maint (BYPASSRLS), like
-- records_matching, and applies the policy's own predicate, `repository =
-- current_setting('substrate.repository', true)`, so it answers exactly the
-- rows the policy would have: an unscoped session matches nothing. NULL kinds
-- is every kind and NULL lim no cap. It returns identities only, in no order;
-- the caller joins them back to `records` under the policy and orders them.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    EXECUTE format($fn$
        CREATE OR REPLACE FUNCTION %1$I.records_matching_kinds(q tsquery, kinds text[], lim integer)
        RETURNS TABLE (kind text, id text)
        LANGUAGE sql STABLE SECURITY DEFINER
        SET search_path = %1$I, pg_temp
        SET enable_seqscan = off
        SET enable_indexscan = off
        AS $body$
            SELECT r.kind, r.id FROM %1$I.records r
            WHERE r.repository = current_setting('substrate.repository', true)
              AND r.deleted_at IS NULL AND r.fts @@ q
              AND (kinds IS NULL OR r.kind = ANY(kinds))
            LIMIT lim
        $body$
    $fn$, sch);
    EXECUTE format('REVOKE ALL ON FUNCTION %I.records_matching_kinds(tsquery, text[], integer) FROM PUBLIC', sch);
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        -- A new owner must hold CREATE on the schema at the moment of the
        -- change; maint is lent it for the one statement.
        IF has_schema_privilege('substrate_maint', sch, 'CREATE') THEN
            EXECUTE format('ALTER FUNCTION %I.records_matching_kinds(tsquery, text[], integer) OWNER TO substrate_maint', sch);
        ELSE
            EXECUTE format('GRANT CREATE ON SCHEMA %I TO substrate_maint', sch);
            EXECUTE format('ALTER FUNCTION %I.records_matching_kinds(tsquery, text[], integer) OWNER TO substrate_maint', sch);
            EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM substrate_maint', sch);
        END IF;
    END IF;
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT EXECUTE ON FUNCTION %I.records_matching_kinds(tsquery, text[], integer) TO substrate_app', sch);
    END IF;
END $$;
