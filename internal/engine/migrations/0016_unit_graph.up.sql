-- The unit graph's derived tables (decision 0186, engine/unitgraph.go).
-- unit_graph and unit_uses are functions of the records the fold writes, kept by the fold in the
-- write's own transaction, so `repository rebuild` clears them and the replay
-- derives them again. `repository verify` and the export never read them.

-- unit_graph: one row per repository, the counter every fold of a
-- `substrate.reamde.dev/core/unit` or `substrate.reamde.dev/core/conversion`
-- row moves by one. A reader takes it inside its own snapshot and uses the
-- graph built for that revision, so a read that began before a conversion
-- committed never sees the new graph halfway. An absent row is revision 0.
CREATE TABLE IF NOT EXISTS unit_graph (
    repository text   NOT NULL DEFAULT current_setting('substrate.repository'),
    revision   bigint NOT NULL,
    PRIMARY KEY (repository)
);

-- unit_uses: one row per record and unit spelling the record's properties
-- store: a quantity anywhere in the record (nested fields, lists and a
-- reference's link properties included), and the top-level `unit` of a
-- packed observed record or a metric. A tombstone keeps its rows, because a
-- restore brings the value back; a purge removes them. The guard that refuses
-- deleting a used unit or splitting two used units apart reads it. A
-- reference to a unit record is the refs index's, not this table's.
CREATE TABLE IF NOT EXISTS unit_uses (
    repository text NOT NULL DEFAULT current_setting('substrate.repository'),
    kind       text NOT NULL,
    record_id  text COLLATE "C" NOT NULL,
    unit       text COLLATE "C" NOT NULL,
    PRIMARY KEY (repository, kind, record_id, unit)
);
-- Whether a spelling is stored anywhere, and in which kind: one index probe
-- per spelling the guard asks about.
CREATE INDEX IF NOT EXISTS unit_uses_unit_idx ON unit_uses (repository, unit, kind);

-- unit_uses_index: which rule derived the unit uses (engine/unitgraph.go
-- unitUsesVersion). A repository whose rows predate this table has no row
-- here, so its open asks the pass behind it to derive every row's uses
-- (index_reprojections.units) and writes the version; a creation and a
-- rebuild derive them as they fold and write it too.
CREATE TABLE IF NOT EXISTS unit_uses_index (
    repository text    NOT NULL DEFAULT current_setting('substrate.repository'),
    version    integer NOT NULL,
    PRIMARY KEY (repository)
);
ALTER TABLE index_reprojections ADD COLUMN IF NOT EXISTS units boolean NOT NULL DEFAULT false;

-- Repository-scoped like every table a dataset writes, with 0011's row level
-- security: the `repository` column defaults to the connection's setting and
-- the policy holds each row to it.
ALTER TABLE unit_graph ENABLE ROW LEVEL SECURITY;
ALTER TABLE unit_graph FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS unit_graph_repository ON unit_graph;
CREATE POLICY unit_graph_repository ON unit_graph FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE unit_uses_index ENABLE ROW LEVEL SECURITY;
ALTER TABLE unit_uses_index FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS unit_uses_index_repository ON unit_uses_index;
CREATE POLICY unit_uses_index_repository ON unit_uses_index FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE unit_uses ENABLE ROW LEVEL SECURITY;
ALTER TABLE unit_uses FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS unit_uses_repository ON unit_uses;
CREATE POLICY unit_uses_repository ON unit_uses FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The grants 0001 gave `ON ALL TABLES` covered what existed then, so a later
-- table carries its own. The app role writes both in the write's transaction;
-- erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
    t   text;
BEGIN
    FOREACH t IN ARRAY ARRAY['unit_graph', 'unit_uses', 'unit_uses_index'] LOOP
        IF to_regrole('substrate_app') IS NOT NULL THEN
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO substrate_app', sch, t);
        END IF;
        IF to_regrole('substrate_maint') IS NOT NULL THEN
            EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO substrate_maint', sch, t);
        END IF;
    END LOOP;
END $$;
