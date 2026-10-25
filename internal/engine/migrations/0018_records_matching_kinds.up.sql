-- The live records of the given kinds a tsquery matches in the session's
-- repository, read through records_fts_idx, at most lim of them.
--
-- A lexical search whose strict pass runs out of its share of the budget
-- reading a common word's matches newest first takes some matches from the
-- index instead (internal/engine/bm25.go, lexicalCandidates). Under the
-- policy the newest-first read walks records_created_at_idx and matches each
-- row it meets, reading the row's whole text vector from TOAST: on a
-- repository of 620k records a word nineteen thousand rows held ran past the
-- whole budget cold. records_matching (migration 0010) answers through the
-- index in tens of milliseconds, but its cap reaches inside before any kind
-- filter, so a search naming kinds that are a small share of the matches got
-- back few of them or none. This function applies the kinds inside, before
-- the cap.
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
