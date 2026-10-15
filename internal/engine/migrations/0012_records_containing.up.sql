-- The live records of one kind in the session's repository whose properties
-- contain a jsonb needle, read through records_props_idx.
--
-- Row level security keeps the index from answering a mapping probe. `@>`
-- and `->>` on jsonb are not leakproof, so under the policy the planner may
-- not evaluate them before the policy's repository qual, and it never probes
-- the GIN index with them: every probe read every row of its target kind.
-- A probe runs inside a write transaction while the repository's changelog
-- lock is held (internal/engine/mapping.go probeCandidates), so a background
-- sync that probes once per source record held the lock for seconds and every
-- other writer waited behind it. On a kind of 200k rows one probe was 75 ms
-- on the row and 0.1 ms through the index.
--
-- The function is SECURITY DEFINER and owned by substrate_maint (BYPASSRLS),
-- like records_matching (migration 0010), and applies the policy's own
-- predicate, `repository = current_setting('substrate.repository', true)`,
-- so it answers exactly the rows the policy would have: an unscoped session
-- matches nothing. It returns ids only, in no order; the caller orders them.
--
-- The body names its arguments by position so a test can plan the very
-- statement under the owner's role (mappingprobe_db_test.go).
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    EXECUTE format($fn$
        CREATE OR REPLACE FUNCTION %1$I.records_containing(k text, needle jsonb)
        RETURNS TABLE (id text)
        LANGUAGE sql STABLE SECURITY DEFINER
        SET search_path = %1$I, pg_temp
        AS $body$
            SELECT r.id FROM %1$I.records r
            WHERE r.repository = current_setting('substrate.repository', true)
              AND r.kind = $1 AND r.deleted_at IS NULL AND r.props @> $2
        $body$
    $fn$, sch);
    EXECUTE format('REVOKE ALL ON FUNCTION %I.records_containing(text, jsonb) FROM PUBLIC', sch);
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        -- A new owner must hold CREATE on the schema at the moment of the
        -- change; maint is lent it for the one statement.
        IF has_schema_privilege('substrate_maint', sch, 'CREATE') THEN
            EXECUTE format('ALTER FUNCTION %I.records_containing(text, jsonb) OWNER TO substrate_maint', sch);
        ELSE
            EXECUTE format('GRANT CREATE ON SCHEMA %I TO substrate_maint', sch);
            EXECUTE format('ALTER FUNCTION %I.records_containing(text, jsonb) OWNER TO substrate_maint', sch);
            EXECUTE format('REVOKE CREATE ON SCHEMA %I FROM substrate_maint', sch);
        END IF;
    END IF;
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT EXECUTE ON FUNCTION %I.records_containing(text, jsonb) TO substrate_app', sch);
    END IF;
END $$;
