-- The records a tsquery matches in the session's repository, read through
-- records_fts_idx.
--
-- Row level security keeps the index from answering the match itself.
-- `tsvector @@ tsquery` (ts_match_vq) is not leakproof, so under the policy
-- the planner may not evaluate it before the policy's repository qual, and it
-- never probes the GIN index with it: every lexical search and every list
-- `search` filter read the whole repository and matched row by row. On a
-- repository of 389k records that was 1.9 s for a word eighteen records hold,
-- against 0.4 ms through the index.
--
-- This function is the one read that runs the match unbound by the policy. It
-- is SECURITY DEFINER and owned by substrate_maint (BYPASSRLS), and it applies
-- the policy's own predicate, `repository = current_setting(
-- 'substrate.repository', true)`, so it answers exactly the rows the policy
-- would have: an unscoped session matches nothing. It returns identities
-- only. The callers join them back to `records` under the policy for every
-- column they read, so the ranking and the list order stay in Go.
--
-- `lim` caps the rows (NULL is no cap): a document frequency counts to a
-- bound, and the cap has to reach inside, because a SECURITY DEFINER function
-- is never inlined into the caller's LIMIT.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    EXECUTE format($fn$
        CREATE OR REPLACE FUNCTION %1$I.records_matching(q tsquery, lim integer)
        RETURNS TABLE (kind text, id text)
        LANGUAGE sql STABLE SECURITY DEFINER
        SET search_path = %1$I, pg_temp
        AS $body$
            SELECT r.kind, r.id FROM %1$I.records r
            WHERE r.repository = current_setting('substrate.repository', true)
              AND r.deleted_at IS NULL AND r.fts @@ q
            LIMIT lim
        $body$
    $fn$, sch);
    EXECUTE format('REVOKE ALL ON FUNCTION %I.records_matching(tsquery, integer) FROM PUBLIC', sch);
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        -- A new owner must hold CREATE on the schema at the moment of the
        -- change; maint is lent it for the one statement.
        IF has_schema_privilege('substrate_maint', sch, 'CREATE') THEN
            EXECUTE format('ALTER FUNCTION %I.records_matching(tsquery, integer) OWNER TO substrate_maint', sch);
        ELSE
            EXECUTE format('GRANT CREATE ON SCHEMA %I TO substrate_maint', sch);
            EXECUTE format('ALTER FUNCTION %I.records_matching(tsquery, integer) OWNER TO substrate_maint', sch);
            EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM substrate_maint', sch);
        END IF;
    END IF;
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT EXECUTE ON FUNCTION %I.records_matching(tsquery, integer) TO substrate_app', sch);
    END IF;
END $$;
