-- The substrate's ONE schema. There is no schema per
-- repository and no search_path trick: every repository-scoped table carries
-- a `repository` column, and the isolation between two repositories is
-- ENFORCED BY POSTGRES, not by discipline in the Go query strings.
--
-- The mechanism is three parts, and they compose so that a query which forgets
-- the repository is a refusal rather than a leak:
--
--   1. `repository text NOT NULL DEFAULT current_setting('substrate.repository')`
--      — an INSERT never names the column; it inherits the connection's
--      repository. A connection with no repository setting cannot insert at
--      all: current_setting raises "unrecognized configuration parameter",
--      which is the loud failure an unscoped write deserves.
--   2. `ENABLE` + `FORCE ROW LEVEL SECURITY` with one FOR ALL policy per table,
--      `repository = current_setting('substrate.repository', true)`. FORCE
--      makes the policy apply to the table's owner too, so nothing but a
--      BYPASSRLS role escapes it, and the missing_ok form fails CLOSED: a
--      session with no setting matches no row rather than erroring on a read.
--   3. Two roles — `substrate_app`, which every repository-scoped pool SETs
--      ROLE to, and `substrate_maint` (BYPASSRLS) for registration, the
--      repository lookup, seeding and rebuild. The background loops use the
--      bypass to ENUMERATE repositories only; their work runs scoped. The roles
--      are created by the engine before this migration runs (engine.go
--      ensureRoles); the grants below are idempotent and schema-local.
--
-- `repositories` is the substrate's only control-plane table — one row per
-- user, the user IS the row — and is the one table `substrate_app` cannot see.
CREATE EXTENSION IF NOT EXISTS vector SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public;

-- The control plane, and the WHOLE of it: one row per user, the user IS the
-- row. `id` is opaque and internal — it appears in substratectl
-- output and nowhere else — and `created_at` is the admission record, since
-- the invite code is the only door and there is nothing else to record. The
-- user's auth material is NOT here: the password hash and the TOTP seed live
-- in `sealed`, referenced from the repository's own `credential` record.
CREATE TABLE IF NOT EXISTS repositories (
    id         text        PRIMARY KEY,
    username   text        NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- `records` is the fold of the changelog: one row per (repository, kind, id), latest
-- state. Identity is the pair (kind, id) WITHIN a repository — an id is unique
-- per kind, never per repository, so every table that names a record carries
-- its kind reference beside the id. The kind is the REFERENCE
-- ("tasks.substrate.reamde.dev/task", or a bare "task" for a repository-local kind).
CREATE TABLE IF NOT EXISTS records (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    kind        text        NOT NULL,
    id          text        NOT NULL,
    title       text        NOT NULL DEFAULT '',
    body        text        NOT NULL DEFAULT '',
    states      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at          timestamptz,
    ends_at     timestamptz,
    due_at      timestamptz,
    props       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    labels      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    deleted_at  timestamptz,
    finalizers  text[]      NOT NULL DEFAULT '{}',
    fts         tsvector,
    PRIMARY KEY (repository, kind, id)
);

CREATE INDEX IF NOT EXISTS records_at_idx         ON records (repository, at);
CREATE INDEX IF NOT EXISTS records_ends_at_idx    ON records (repository, ends_at);
CREATE INDEX IF NOT EXISTS records_due_at_idx     ON records (repository, due_at);
CREATE INDEX IF NOT EXISTS records_created_at_idx ON records (repository, created_at);
CREATE INDEX IF NOT EXISTS records_updated_at_idx ON records (repository, updated_at);
CREATE INDEX IF NOT EXISTS records_deleted_at_idx ON records (repository, deleted_at);
CREATE INDEX IF NOT EXISTS records_props_idx      ON records USING gin (props jsonb_path_ops);
CREATE INDEX IF NOT EXISTS records_labels_idx     ON records USING gin (labels jsonb_path_ops);
CREATE INDEX IF NOT EXISTS records_states_idx     ON records USING gin (states jsonb_path_ops);
CREATE INDEX IF NOT EXISTS records_fts_idx        ON records USING gin (fts);

-- Edges name both endpoints in full: (src_kind, src) -> (dst_kind, dst).
-- `subject` marks the mapping-created link; the partial unique index keeps a
-- source record on exactly one subject edge, per repository.
CREATE TABLE IF NOT EXISTS edges (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    rel        text        NOT NULL,
    src_kind   text        NOT NULL,
    src        text        NOT NULL,
    dst_kind   text        NOT NULL,
    dst        text        NOT NULL,
    props      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    subject    boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, rel, src_kind, src, dst_kind, dst)
);
CREATE INDEX IF NOT EXISTS edges_dst_idx ON edges (repository, dst_kind, dst);
CREATE INDEX IF NOT EXISTS edges_src_idx ON edges (repository, src_kind, src);
CREATE UNIQUE INDEX IF NOT EXISTS edges_subject_uniq ON edges (repository, src_kind, src, rel) WHERE subject;

-- Merge trails, flattened: a former id resolves WITHIN ITS TYPE (merge only
-- ever joins two records of one kind), so a read by a former id is one typed
-- lookup.
CREATE TABLE IF NOT EXISTS former_ids (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    former_id   text        NOT NULL,
    record_id   text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, record_kind, former_id)
);
CREATE INDEX IF NOT EXISTS former_ids_record_idx ON former_ids (repository, record_kind, record_id);

CREATE TABLE IF NOT EXISTS annotations (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    key         text        NOT NULL,
    value       jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, key)
);

-- Which actor last had a change accepted per property, and at which tier that
-- write stood. The tier is written on every accepted write, so it is NOT NULL.
CREATE TABLE IF NOT EXISTS property_managers (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    property    text        NOT NULL,
    actor       text        NOT NULL,
    tier        text        NOT NULL,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property)
);

-- Property offers: recompute's projection of what each live source's actor
-- would write — the rows behind propertyMeta's alternatives.
CREATE TABLE IF NOT EXISTS property_offers (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    property    text        NOT NULL,
    actor       text        NOT NULL,
    value       jsonb,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property, actor)
);
CREATE INDEX IF NOT EXISTS property_offers_record ON property_offers (repository, record_kind, record_id);

-- The changelog: the per-repository append-only sequence of changes, and the source
-- of truth the records table folds. `seq` is PER REPOSITORY and assigned at
-- commit under the repository's own advisory lock (rows.go appendChange), so
-- two repositories never share, skip or collide on a cursor value.
-- caused_by is the seq that caused a function-authored write; NULL on every
-- direct write, so the causal-depth walk terminates.
CREATE TABLE IF NOT EXISTS changelog (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    seq        bigint      NOT NULL,
    ts         timestamptz NOT NULL DEFAULT now(),
    actor      text        NOT NULL,
    op         text        NOT NULL,
    record_id  text        NOT NULL,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    caused_by  bigint,
    PRIMARY KEY (repository, seq)
);
CREATE INDEX IF NOT EXISTS changelog_record_idx ON changelog (repository, record_id);
CREATE INDEX IF NOT EXISTS changelog_kind_idx   ON changelog (repository, kind);

CREATE TABLE IF NOT EXISTS embeddings (
    repository  text NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text NOT NULL,
    record_id   text NOT NULL,
    property    text NOT NULL,
    chunk       int  NOT NULL,
    text_hash   text NOT NULL,
    vec         public.vector(1536),
    PRIMARY KEY (repository, record_kind, record_id, property, chunk)
);

-- `generation` is the source-text fence. A property's queue row is one per
-- (repository, record, property); an edit DURING a slow embed re-enqueues and
-- INCREMENTS the generation, so the worker — which snapshots the generation
-- before it embeds — can tell, when it comes to write, whether the text it
-- embedded is still the current text. It writes the vectors and drops the row
-- only while the generation it snapshotted still stands; a bumped generation
-- leaves the newer job pending and the stale vectors uncommitted.
CREATE TABLE IF NOT EXISTS embed_queue (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    property    text        NOT NULL,
    generation  bigint      NOT NULL DEFAULT 1,
    enqueued_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, record_kind, record_id, property)
);

-- Trigger delivery bookkeeping. Still tables, never records: a cursor must not
-- be matchable by a `*` subscription.
CREATE TABLE IF NOT EXISTS trigger_cursors (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    seq        bigint      NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (repository, trigger_id)
);

CREATE TABLE IF NOT EXISTS trigger_failures (
    id         bigserial   PRIMARY KEY,
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    seq        bigint      NOT NULL,
    record_id  text        NOT NULL,
    attempts   integer     NOT NULL,
    last_error text        NOT NULL,
    parked_at  timestamptz NOT NULL,
    fire_id    text        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS trigger_failures_trigger ON trigger_failures (repository, trigger_id, parked_at);

CREATE TABLE IF NOT EXISTS trigger_schedule (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    fired_at   timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (repository, trigger_id)
);

-- The sealed store: secret material wrapped under the substrate master key.
-- Records carry secret-typed REFS into this table, never raw tokens; nothing
-- here is ever in the changelog or in the fold's data. expires_at is denormalized
-- off the payload so the refresh loop queries cheaply.
CREATE TABLE IF NOT EXISTS sealed (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    ref         text        NOT NULL,
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    payload     bytea       NOT NULL,
    expires_at  timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, ref)
);
CREATE INDEX IF NOT EXISTS sealed_record ON sealed (repository, record_kind, record_id);
CREATE INDEX IF NOT EXISTS sealed_expiry ON sealed (repository, expires_at);

-- Pending OAuth flows: one row per started connect flow, keyed by the sha256
-- of the state's random nonce. The callback consumes its row atomically
-- (DELETE … RETURNING), which is what makes a signed state one-time.
CREATE TABLE IF NOT EXISTS oauth_flows (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    nonce_hash  text        NOT NULL,
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    verifier    bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, nonce_hash)
);
CREATE INDEX IF NOT EXISTS oauth_flows_expiry ON oauth_flows (repository, expires_at);

-- Paged-checkpoint invocation: a delivery whose body returns "more work + a
-- resume cursor" is re-invoked off the causal chain until drained, each page's
-- effects committed with the cursor. `version` is the cursor-ownership fence;
-- `effects`/`bytes`/`started_at` are the cumulative drain budget;
-- `trigger_id`/`kind`/`identity` give the row a lifecycle owner.
CREATE TABLE IF NOT EXISTS paged_cursors (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    chain      text        NOT NULL,
    cursor     jsonb       NOT NULL,
    pages      bigint      NOT NULL DEFAULT 0,
    version    bigint      NOT NULL DEFAULT 0,
    effects    bigint      NOT NULL DEFAULT 0,
    bytes      bigint      NOT NULL DEFAULT 0,
    started_at timestamptz NOT NULL,
    trigger_id text        NOT NULL DEFAULT '',
    kind       text        NOT NULL DEFAULT '',
    identity   text        NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (repository, chain)
);
CREATE INDEX IF NOT EXISTS paged_cursors_trigger ON paged_cursors (repository, trigger_id);

-- The content-addressed byte store: one row per distinct digest PER
-- REPOSITORY. There is no cross-repository deduplication — a digest in one
-- repository is invisible to another, and the RLS policy is what makes that
-- true rather than a convention.
CREATE TABLE IF NOT EXISTS blobs (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    digest     text        NOT NULL,   -- "blob-sha256-<hex>", also the blob record id
    mime_type  text        NOT NULL DEFAULT '',
    size       bigint      NOT NULL,
    bytes      bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, digest)
);

-- The stored-schema dialect: one row per repository, a monotonic integer
-- stamped by the binary when the repository opens.
CREATE TABLE IF NOT EXISTS vocabulary_dialect (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    dialect    integer     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

CREATE TABLE IF NOT EXISTS vocabulary_promotions (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    dialect    integer     NOT NULL,
    name       text        NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, dialect)
);

-- Row level security: ENABLE plus FORCE (so the owner is bound too), one FOR
-- ALL policy per repository-scoped table. The missing_ok form of
-- current_setting means an unscoped session matches nothing instead of
-- erroring — it fails closed.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'records', 'edges', 'former_ids', 'annotations', 'property_managers',
        'property_offers', 'changelog', 'embeddings', 'embed_queue',
        'trigger_cursors', 'trigger_failures', 'trigger_schedule', 'sealed',
        'oauth_flows', 'paged_cursors', 'blobs', 'vocabulary_dialect',
        'vocabulary_promotions'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', t || '_repository', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR ALL'
            || ' USING (repository = current_setting(''substrate.repository'', true))'
            || ' WITH CHECK (repository = current_setting(''substrate.repository'', true))',
            t || '_repository', t);
    END LOOP;
END $$;

-- Grants, schema-local so parallel test schemas in one cluster do not fight.
-- substrate_app gets the repository-scoped tables and nothing else: the
-- control-plane table and the DDL ledger are maint's alone.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO substrate_app', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO substrate_app', sch);
        EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO substrate_app', sch);
        EXECUTE format('REVOKE ALL ON TABLE %I.repositories FROM substrate_app', sch);
        EXECUTE format('REVOKE ALL ON TABLE %I.schema_migrations FROM substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO substrate_maint', sch);
        EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO substrate_maint', sch);
    END IF;
END $$;
