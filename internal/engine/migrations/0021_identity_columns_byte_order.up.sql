-- Every column that holds a record's kind or id, or the id of the repository
-- it lives in, sorts COLLATE "C", plain byte order: `kind` and `id` in
-- records, the kind and id columns of every table keyed by one record, or
-- indexed by one, a trigger's tables keyed by its record id included, the
-- `repository` column of every table, and the control plane's repository id
-- (the 1.0 review, #1313, F14). So every primary key leads with a column in
-- byte order.
--
-- Under the database's own collation (en_US in the pgvector image) the
-- primary key of records, and every paged listing over it, orders case and
-- punctuation differently from Go's string comparison, so a read that merges
-- a page with values Go sorted (the window read's computed occurrences) or
-- resumes from a cursor Go compared could disagree with the index. And the
-- order depended on the host's locale library: Postgres warns that an index
-- built under one version of it can be wrong under the next. The observed
-- index and unit_uses already sort their record ids this way (migrations 0013
-- and 0016).
--
-- The columns are NAMED, and only a column of a base table in this schema
-- whose type is plain text is changed: a table, a view or a domain somebody
-- added to the database is not this server's to change. A column already in
-- byte order is skipped, so the migration meets a schema that has the change
-- and does nothing.
--
-- The values and the table rows do not change, only how they sort, so no
-- table is rewritten: each index over a changed column is rebuilt, once per
-- table, because a table's columns change in one statement. Postgres refuses
-- to change a column a row security policy reads, whichever table the policy
-- is on, so every policy that reads one of the columns is dropped before the
-- first change and created again, from what pg_policies held, after the last:
-- all in this one transaction, so no session ever sees a table without them.
-- A view or a rule somebody added over one of these columns still refuses the
-- change, and the whole migration rolls back.
--
-- observed_streams' metric, kind, source and about stay as they are: they key
-- a stream, not a record.

-- No other server runs while this does. A running server holds positions
-- in memory that compare these columns (a search reindex's last id, a
-- reprojection's checkpoint, a page a walk has not yet asked for), and one
-- that crossed the change of order would resume past the rows that now sort
-- before it and record its pass as done. Rather than find and fence each such
-- position, the migration refuses while any server holds a repository: the
-- writer lease every server takes on each repository it opens (writerlease.go,
-- decision 0083), a session advisory lock keyed by the repository's id, or by
-- its name on a release before ids. It takes each of those keys itself for
-- its transaction, so a server cannot take one until it commits; a key
-- another session holds refuses the migration, which then changes nothing.
--
-- `repositories` is locked first, in the mode its change below takes anyway,
-- so the scan sees every repository there will be until the commit. A
-- registration takes its lease before it writes its row: one whose row
-- committed before this lock is in the scan, and its lease refuses the
-- migration; one that has not is held at the table until the commit, and
-- what it creates afterwards holds no position from before.
--
-- What the refusal sees is a lease held now. A server whose connection to the
-- database is down at this moment holds none, and retakes its leases when the
-- database answers again, its passes still where they were. The guard is for
-- the ordinary case; stopping every running server before starting this
-- release is the instruction.
LOCK TABLE repositories IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
    repo record;
BEGIN
    FOR repo IN SELECT id, authority FROM repositories ORDER BY id LOOP
        IF NOT pg_try_advisory_xact_lock(hashtext(current_schema() || '|writer|' || repo.id)::bigint)
           OR NOT pg_try_advisory_xact_lock(hashtext(current_schema() || '|writer|' || repo.authority)::bigint) THEN
            RAISE EXCEPTION 'another server is running against this database and holds repository %: stop every running server, then start this release; migration 0021 changed nothing', repo.authority;
        END IF;
    END LOOP;
END $$;

DO $$
DECLARE
    byte_order oid := (SELECT oid FROM pg_collation
                       WHERE collname = 'C' AND collnamespace = 'pg_catalog'::regnamespace);
    relids oid[];
    attnums int2[];
    tbl record;
    pol record;
    again text[] := ARRAY[]::text[];
    stmt text;
BEGIN
    SELECT array_agg(c.oid ORDER BY c.relname, a.attname), array_agg(a.attnum ORDER BY c.relname, a.attname)
    INTO relids, attnums
    FROM (VALUES
        ('records', 'kind'), ('records', 'id'),
        ('refs', 'src_kind'), ('refs', 'src'), ('refs', 'dst_kind'), ('refs', 'dst'),
        ('former_ids', 'record_kind'), ('former_ids', 'record_id'), ('former_ids', 'former_id'),
        ('annotations', 'record_kind'), ('annotations', 'record_id'),
        ('property_managers', 'record_kind'), ('property_managers', 'record_id'),
        ('property_offers', 'record_kind'), ('property_offers', 'record_id'),
        ('embeddings', 'record_kind'), ('embeddings', 'record_id'),
        ('embed_queue', 'record_kind'), ('embed_queue', 'record_id'),
        ('sealed', 'record_kind'), ('sealed', 'record_id'),
        ('oauth_flows', 'record_kind'), ('oauth_flows', 'record_id'),
        ('trigger_failures', 'record_id'), ('trigger_failures', 'trigger_id'),
        ('trigger_cursors', 'trigger_id'), ('trigger_schedule', 'trigger_id'),
        ('paged_cursors', 'trigger_id'),
        ('changelog', 'kind'), ('changelog', 'record_id'),
        ('observed_segments', 'kind'),
        ('unit_uses', 'kind'),
        ('index_reprojections', 'kind'), ('index_reprojections', 'after_id'),
        ('repositories', 'id'),
        ('records', 'repository'), ('refs', 'repository'), ('former_ids', 'repository'),
        ('annotations', 'repository'), ('property_managers', 'repository'),
        ('property_offers', 'repository'), ('changelog', 'repository'),
        ('embeddings', 'repository'), ('embed_queue', 'repository'),
        ('trigger_cursors', 'repository'), ('trigger_failures', 'repository'),
        ('trigger_schedule', 'repository'), ('sealed', 'repository'),
        ('oauth_flows', 'repository'), ('paged_cursors', 'repository'),
        ('vocabulary_dialect', 'repository'), ('changelog_dialect', 'repository'),
        ('import_progress', 'repository'), ('idempotency_keys', 'repository'),
        ('repository_migrations', 'repository'), ('search_index', 'repository'),
        ('index_reprojections', 'repository'), ('observed_streams', 'repository'),
        ('observed_segments', 'repository'), ('observed_rollups', 'repository'),
        ('observed_latest', 'repository'), ('observed_index', 'repository'),
        ('unit_graph', 'repository'), ('unit_uses', 'repository'),
        ('unit_uses_index', 'repository'), ('operator_spend_reservations', 'repository')
    ) AS want(table_name, column_name)
    JOIN pg_class c ON c.relname = want.table_name AND c.relkind = 'r'
        AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = current_schema())
    JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = want.column_name AND NOT a.attisdropped
    WHERE a.atttypid = 'text'::regtype AND a.attcollation IS DISTINCT FROM byte_order;

    IF relids IS NULL THEN
        RETURN;
    END IF;

    FOR pol IN
        SELECT DISTINCT p.schemaname, p.tablename, p.policyname, p.permissive, p.cmd, p.roles, p.qual, p.with_check
        FROM unnest(relids, attnums) AS t(relid, attnum)
        JOIN pg_depend d ON d.refclassid = 'pg_class'::regclass AND d.refobjid = t.relid
            AND d.refobjsubid = t.attnum AND d.classid = 'pg_policy'::regclass
        JOIN pg_policy pp ON pp.oid = d.objid
        JOIN pg_class pc ON pc.oid = pp.polrelid
        JOIN pg_namespace pn ON pn.oid = pc.relnamespace
        JOIN pg_policies p ON p.schemaname = pn.nspname
            AND p.tablename = pc.relname AND p.policyname = pp.polname
        ORDER BY p.schemaname, p.tablename, p.policyname
    LOOP
        again := again || format('CREATE POLICY %I ON %I.%I AS %s FOR %s TO %s%s%s',
            pol.policyname, pol.schemaname, pol.tablename, pol.permissive, pol.cmd,
            (SELECT string_agg(CASE WHEN r = 'public' THEN 'PUBLIC' ELSE quote_ident(r) END, ', ')
               FROM unnest(pol.roles) AS r),
            CASE WHEN pol.qual IS NULL THEN '' ELSE ' USING (' || pol.qual || ')' END,
            CASE WHEN pol.with_check IS NULL THEN '' ELSE ' WITH CHECK (' || pol.with_check || ')' END);
        EXECUTE format('DROP POLICY %I ON %I.%I', pol.policyname, pol.schemaname, pol.tablename);
    END LOOP;

    FOR tbl IN
        SELECT c.relname AS table_name,
               string_agg(format('ALTER COLUMN %I TYPE text COLLATE "C"', a.attname), ', ' ORDER BY a.attname) AS alters
        FROM unnest(relids, attnums) AS t(relid, attnum)
        JOIN pg_class c ON c.oid = t.relid
        JOIN pg_attribute a ON a.attrelid = t.relid AND a.attnum = t.attnum
        GROUP BY c.relname
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %I %s', tbl.table_name, tbl.alters);
    END LOOP;

    FOREACH stmt IN ARRAY again LOOP
        EXECUTE stmt;
    END LOOP;

    -- Changing a column's type, its collation included, drops the planner's
    -- statistics for it, and nothing gathers them again until autovacuum
    -- comes round: until then every read and join over a kind, an id or a
    -- repository is planned on fallback guesses. Each changed column is
    -- analyzed again here, in this transaction, under the locks it already
    -- holds. ANALYZE reads a fixed sample (300 rows per point of the
    -- statistics target, 30,000 at the default), so its cost does not grow
    -- with a table's size.
    FOR tbl IN
        SELECT c.relname AS table_name,
               string_agg(quote_ident(a.attname), ', ' ORDER BY a.attname) AS cols
        FROM unnest(relids, attnums) AS t(relid, attnum)
        JOIN pg_class c ON c.oid = t.relid
        JOIN pg_attribute a ON a.attrelid = t.relid AND a.attnum = t.attnum
        GROUP BY c.relname
        ORDER BY c.relname
    LOOP
        EXECUTE format('ANALYZE %I (%s)', tbl.table_name, tbl.cols);
    END LOOP;
END $$;

-- A reprojection the server had not finished resumes after the id its last
-- page ended at, read with `id >` in the column's order (reprojection.go
-- reprojectionPage). That order is byte order from here, so a page that
-- ended at `b` under the locale's order would leave `Zed`, which now sorts
-- before it, never re-derived, and the request deleted when the pass ends.
-- Every pending pass starts again from the first row instead: a row derived
-- twice derives the same, so the cost is the work repeated. `requested_at`
-- moves with it, because it is the fence a pass advances and deletes its
-- request under: a pass another server was running when this migration
-- committed (a second boot migrates before it is refused the repository)
-- still holds the old checkpoint in memory, and its advance and its delete
-- now match nothing, so the restarted request survives it. The table is the
-- server's list of owed work, not a fold table, so the rebuild does not clear
-- it and a migration may write it. Row security holds even the table's owner
-- to one repository, so it is set aside for the one statement, inside this
-- transaction.
ALTER TABLE index_reprojections NO FORCE ROW LEVEL SECURITY;
UPDATE index_reprojections SET after_id = '', requested_at = now();
ALTER TABLE index_reprojections FORCE ROW LEVEL SECURITY;
