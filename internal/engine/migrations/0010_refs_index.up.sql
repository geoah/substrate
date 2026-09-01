-- The refs index: the reverse projection of every `type: reference` value a
-- record holds. It REPLACES the edges table, which held the other half of a
-- concept references now carry alone (decision 0044).
--
-- It is DERIVED, exactly as `records` is: one function computes a record's
-- rows from its folded properties and its kind's declaration (engine/refs.go),
-- the write path re-derives after the fold, and a rebuild re-derives after the
-- replay. Nothing writes it from a changelog effect, so there is no second
-- description of what a reference does to storage to drift from the values.
--
-- Named `refs`, not `references`: `references` is a reserved word in Postgres
-- and every statement naming it would have to quote it.
--
-- The DROP is unconditional and takes the data with it. Old changelogs
-- carrying `link`/`unlink` ops or edge fold effects are no longer replayable
-- and the engine refuses to open such a repository (engine/fold.go
-- foldRefuses), so there is nothing here to migrate: an edge row's meaning now
-- lives in the source record's own properties, which no edge row carries.
DROP TABLE IF EXISTS edges;

-- ADDRESSING. A row is one reference VALUE at one SITE in one record:
--
--   property  the kind's own top-level property name;
--   path      the value address BELOW that property, '' for the property
--             itself: object field names, list indices and keyed-map keys
--             joined by dots ('callable', '0.callable', 'work'). Each segment
--             is escaped before it is joined, JSON-Pointer style ('~' -> '~0',
--             '.' -> '~1'), because a keyed map's keys are free text: without
--             it the key 'a.b' holding a field 'c' and the key 'a' holding a
--             nested 'b.c' would spell the same address and collide in the
--             primary key below. engine/refs.go joinRefPath is the one writer;
--   ord       the index of the value inside a repeated reference, 0 otherwise.
--
-- Those three plus the source record are the primary key, so a re-derive of
-- one record replaces exactly its own rows and a reader can page the whole
-- index on a stable order.
--
-- The path is an OPAQUE ADDRESS. `incoming` serves it as stored and its cursor
-- compares it byte-wise; nothing decodes it back into segments.
CREATE TABLE IF NOT EXISTS refs (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    src_kind   text        NOT NULL,
    src        text        NOT NULL,
    property   text        NOT NULL,
    path       text        NOT NULL DEFAULT '',
    ord        integer     NOT NULL DEFAULT 0,
    dst_kind   text        NOT NULL,
    dst        text        NOT NULL,
    props      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, src_kind, src, property, path, ord)
);

-- The reverse read: `incoming`, and the GC cascade, both stand on the TARGET
-- and ask who points at it.
CREATE INDEX IF NOT EXISTS refs_dst_idx ON refs (repository, dst_kind, dst);

-- Row level security, the same shape every repository-scoped table carries
-- (0001).
ALTER TABLE refs ENABLE ROW LEVEL SECURITY;
ALTER TABLE refs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS refs_repository ON refs;
CREATE POLICY refs_repository ON refs FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- 0001 granted the tables that existed when it ran, so a table added later
-- carries its own grants. The app role writes the index on every record write;
-- erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.refs TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.refs TO substrate_maint', sch);
    END IF;
END $$;
