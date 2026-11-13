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
END $$;
