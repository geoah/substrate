-- A property's mark (decision 0284): whether sync may replace the value, and
-- where it came from, as two facts stored apart. It replaces the manager row,
-- whose `actor` answered three questions and whose `tier` was a yes or no:
--
--   pinned        sync leaves the value alone, stored when the value is
--                 written and never worked out again
--   set_by        the actor record that wrote the value directly, '' where
--                 sync wrote it
--   on_behalf_of  the actor record set_by acted for, '' where none
--   derived_from  a JSON list of the record paths of the live sources whose
--                 offers back the value, empty where none does
--   set_at        when the value was set
--
-- A FOLD TABLE, written by the `mark` and `unmark` effects (engine/fold.go).
-- It is created empty: a history written before it holds `manager` effects,
-- which the changelog conversion rewrites into marks, and the fold is rebuilt
-- from the converted lines before the repository opens (engine/conversion.go).
-- So the old table goes whole, and no row moves across in SQL. Every key
-- column sorts COLLATE "C", byte order, whatever the database's locale: an
-- identity compares as bytes everywhere else it is read.
CREATE TABLE IF NOT EXISTS property_marks (
    repository   text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind  text        COLLATE "C" NOT NULL,
    record_id    text        COLLATE "C" NOT NULL,
    property     text        COLLATE "C" NOT NULL,
    pinned       boolean     NOT NULL,
    set_by       text        NOT NULL,
    on_behalf_of text        NOT NULL,
    derived_from jsonb       NOT NULL,
    set_at       timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property)
);
DROP TABLE IF EXISTS property_managers;

-- The offers, keyed by the source record that offers each value rather than
-- by the actor that wrote it, so two accounts of one provider, synced by one
-- function, are two alternatives. DERIVED STORAGE: recompute writes it, the
-- changelog never carries it, and a rebuild derives it again from the fold
-- (engine/rebuild.go rederiveOffers), as the conversion's rebuild does for a
-- store this migration meets. So the table is replaced, not re-keyed in
-- place: a row from before 0004 holds no source to key on.
DROP TABLE IF EXISTS property_offers;
CREATE TABLE property_offers (
    repository  text        COLLATE "C" NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        COLLATE "C" NOT NULL,
    record_id   text        COLLATE "C" NOT NULL,
    property    text        COLLATE "C" NOT NULL,
    source      text        COLLATE "C" NOT NULL,
    value       jsonb,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property, source)
);
CREATE INDEX IF NOT EXISTS property_offers_record ON property_offers (repository, record_kind, record_id);

-- Repository-scoped like every table a dataset writes, with 0011's row level
-- security: the `repository` column defaults to the connection's setting and
-- the policy holds each row to it.
ALTER TABLE property_marks ENABLE ROW LEVEL SECURITY;
ALTER TABLE property_marks FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS property_marks_repository ON property_marks;
CREATE POLICY property_marks_repository ON property_marks FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));
ALTER TABLE property_offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE property_offers FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS property_offers_repository ON property_offers;
CREATE POLICY property_offers_repository ON property_offers FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- The manager table's name, read-only, for the landed repository migration
-- 0005 alone: it is frozen (frozen:check) and still asks which records a hand
-- holds a property on, in the tiers of the table it was written against, so
-- the view gives them back as that table held them. A mark nothing pinned is
-- `machine`. A pin is `owner` where the person set it (by its actor id, fixed
-- for every repository: an import of a directory from before actors were
-- records runs 0005 before repository migration 9 writes the actor records),
-- where an app did, or where a pin action kept a value no hand set; one a
-- function or an agent set is `bundle`. Nothing else reads it.
-- security_invoker holds it to the reader's own repository, as the tables'
-- policies do.
-- It lives as long as repository migration 0005 can run, which an import of
-- a directory or an archive from before it does. Postgres alters no column a
-- view reads: a later SQL migration that alters one of its columns (the
-- identity columns of property_marks and records, which 0021 put in byte
-- order) drops the view first and creates it again after, in the same
-- migration. 0021 itself runs before this one, so a real upgrade never
-- meets the view there.
CREATE OR REPLACE VIEW property_managers WITH (security_invoker = true) AS
    SELECT m.repository, m.record_kind, m.record_id, m.property, m.set_by AS actor,
           CASE
               WHEN NOT m.pinned THEN 'machine'
               WHEN m.set_by IN ('', '6nk6ekkczdgh399nt2pqpccjz0') THEN 'owner'
               WHEN a.props->>'type' IN ('person', 'app') THEN 'owner'
               ELSE 'bundle'
           END AS tier
    FROM property_marks m
    LEFT JOIN records a ON a.repository = m.repository AND a.kind = 'substrate.reamde.dev/core/actor' AND a.id = m.set_by;

-- The grants 0001 gave `ON ALL TABLES` covered what existed then, so a later
-- table carries its own. The app role folds and derives both; a rebuild and
-- erasing a repository run on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.property_marks TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.property_offers TO substrate_app', sch);
        EXECUTE format('GRANT SELECT ON TABLE %I.property_managers TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.property_marks TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.property_offers TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT ON TABLE %I.property_managers TO substrate_maint', sch);
    END IF;
END $$;
