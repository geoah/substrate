-- The substrate's ONE schema. There is no schema per repository and no
-- search_path trick: every repository-scoped table carries a `repository`
-- column, and the isolation between two repositories is ENFORCED BY POSTGRES,
-- not by discipline in the Go query strings.
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
--      ensureRoles); the grants at the foot of this file are schema-local.
--
-- `repositories` is the substrate's only control-plane table — one row per
-- user, the user IS the row — and is the one table `substrate_app` cannot see.
CREATE EXTENSION IF NOT EXISTS vector SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public;

-- The control plane, and the WHOLE of it: one row per user, the user IS the
-- row.
--
-- `id` IS the repository's authority, the one DNS-style name registration
-- chose: the primary key, every scope, the DEK wrap's binding and the
-- directory under the data root are that name (decision records 0046 and
-- 0052). `authority` carries it beside the id and the CHECK holds the two
-- equal; the engine writes both, and no trigger fills one from the other. The
-- authority is unique across the substrate because a kind reference names its
-- authority and nothing else. The user logs in with that same name (0074):
-- there is no separate username.
--
-- `created_at` is the admission record, since the invite code is the only door
-- and there is nothing else to record. The user's auth material is NOT here:
-- the password hash and the TOTP seed live in `sealed`, referenced from the
-- repository's own `credential` record.
--
-- `dek` is the repository's data-encryption key, wrapped: the sealed store's
-- payloads encrypt under the repository's own random key, and this column
-- holds that key wrapped under the host credential key. The user plane never
-- depends on the host key: the `recoverykey` record inside the repository
-- carries the same DEK wrapped to the user's age recipient. `dek_key_id` names
-- WHICH SUBSTRATE_CREDENTIAL_KEY the bytes are wrapped under, 16 hex digits of
-- a one-way hash over the key material and never the key, so a host holding
-- another key is told which key the wrap wants instead of "no key opens it".
-- `history_generation` is an opaque marker naming the numbering of the
-- repository's changelog: a change cursor is resumable only under the
-- generation it was read from, because an import into a database that holds no
-- row restarts the seqs below cursors clients already saved (decision 0056).
-- The engine mints a fresh value whenever it writes the row, at registration
-- and at import; a restart and `repository rebuild` leave it alone.
CREATE TABLE repositories (
    id                 text        PRIMARY KEY,
    created_at         timestamptz NOT NULL DEFAULT now(),
    dek                bytea,
    authority          text        NOT NULL,
    history_generation text        NOT NULL,
    dek_key_id         text,
    CONSTRAINT repositories_id_is_authority CHECK (id = authority)
);
CREATE UNIQUE INDEX repositories_authority_key ON repositories (authority);

-- `records` is the fold of the changelog: one row per (repository, kind, id), latest
-- state. Identity is the pair (kind, id) WITHIN a repository — an id is unique
-- per kind, never per repository, so every table that names a record carries
-- its kind reference beside the id. The kind is the REFERENCE
-- ("ada.example.com/tasks/task").
--
-- `version` is the edit counter a write asserts with `ifVersion`.
-- `kind_version` is a different number: the kind's EFFECTIVE version at the
-- write that last moved the row, the kind's own pin where it has one, else its
-- package's (internal/vocabulary/load.go). It travels in the `record` delta
-- and the fold restores it, so a rebuild reproduces it and nothing recomputes
-- it from the live registry. 0 is the absent stamp, the spelling declaration
-- versions use for "none stored" (decision 0002).
CREATE TABLE records (
    repository   text        NOT NULL DEFAULT current_setting('substrate.repository'),
    kind         text        NOT NULL,
    id           text        NOT NULL,
    title        text        NOT NULL DEFAULT '',
    body         text        NOT NULL DEFAULT '',
    states       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at           timestamptz,
    ends_at      timestamptz,
    due_at       timestamptz,
    props        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    labels       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    version      bigint      NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz,
    finalizers   text[]      NOT NULL DEFAULT '{}',
    fts          tsvector,
    kind_version bigint      NOT NULL DEFAULT 0,
    PRIMARY KEY (repository, kind, id)
);

CREATE INDEX records_at_idx         ON records (repository, at);
CREATE INDEX records_ends_at_idx    ON records (repository, ends_at);
CREATE INDEX records_due_at_idx     ON records (repository, due_at);
CREATE INDEX records_created_at_idx ON records (repository, created_at);
CREATE INDEX records_updated_at_idx ON records (repository, updated_at);
CREATE INDEX records_deleted_at_idx ON records (repository, deleted_at);
CREATE INDEX records_props_idx      ON records USING gin (props jsonb_path_ops);
CREATE INDEX records_labels_idx     ON records USING gin (labels jsonb_path_ops);
CREATE INDEX records_states_idx     ON records USING gin (states jsonb_path_ops);
CREATE INDEX records_fts_idx        ON records USING gin (fts);

-- The refs index: the reverse projection of every `type: reference` value a
-- record holds. A reference is the only link between records (decision 0044).
--
-- It is DERIVED, exactly as `records` is: one function computes a record's
-- rows from its folded properties and its kind's declaration (engine/refs.go),
-- the write path re-derives after the fold, and a rebuild re-derives after the
-- replay. Nothing writes it from a changelog effect, so there is no second
-- description of what a reference does to storage to drift from the values.
-- Every column is a function of the row's own durable state, timestamps
-- included, which is what makes a live re-projection and a replay produce the
-- same table.
--
-- Named `refs`, not `references`: `references` is a reserved word in Postgres
-- and every statement naming it would have to quote it.
--
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
CREATE TABLE refs (
    repository text    NOT NULL DEFAULT current_setting('substrate.repository'),
    src_kind   text    NOT NULL,
    src        text    NOT NULL,
    property   text    NOT NULL,
    path       text    NOT NULL DEFAULT '',
    ord        integer NOT NULL DEFAULT 0,
    dst_kind   text    NOT NULL,
    dst        text    NOT NULL,
    props      jsonb   NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (repository, src_kind, src, property, path, ord)
);

-- The reverse read: `incoming`, and the GC cascade, both stand on the TARGET
-- and ask who points at it. The index covers the SORT as well as the match:
-- `incoming` matches on (repository, dst_kind, dst) and pages ORDER BY
-- (src_kind, src, property, path, ord), so the ordering key is appended to the
-- match key in the order the query asks for it, and the index CAN answer a
-- page in key order. Whether it does is the reader's half: the target
-- predicate has to be a scalar (`r.dst = $n`) for the planner to walk the
-- index in order, so engine/query.go binds one id that way and keeps the set
-- form only for a record with a former-id trail.
CREATE INDEX refs_dst_idx
    ON refs (repository, dst_kind, dst, src_kind, src, property, path, ord);

-- Merge trails, flattened: a former id resolves WITHIN ITS TYPE (merge only
-- ever joins two records of one kind), so a read by a former id is one typed
-- lookup.
CREATE TABLE former_ids (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    former_id   text        NOT NULL,
    record_id   text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, record_kind, former_id)
);
CREATE INDEX former_ids_record_idx ON former_ids (repository, record_kind, record_id);

CREATE TABLE annotations (
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
--
-- `principal` is the token id the door verified for the write, where the actor
-- is only what the caller asserted. Empty is the one spelling for "no
-- principal on this row": no token stood behind the write (the seed, the boot
-- upgrade, a background worker, registration and login). A fold row is
-- replaced wholesale by the next replay, so one "unknown" is enough for it,
-- unlike the changelog, which is history and keeps its own placeholder.
CREATE TABLE property_managers (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    property    text        NOT NULL,
    actor       text        NOT NULL,
    tier        text        NOT NULL,
    updated_at  timestamptz NOT NULL,
    principal   text        NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property)
);

-- Property offers: recompute's projection of what each live source's actor
-- would write — the rows behind propertyMeta's alternatives.
CREATE TABLE property_offers (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    property    text        NOT NULL,
    actor       text        NOT NULL,
    value       jsonb,
    updated_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, record_kind, record_id, property, actor)
);
CREATE INDEX property_offers_record ON property_offers (repository, record_kind, record_id);

-- The changelog: the per-repository append-only sequence of changes, and the source
-- of truth the records table folds. `seq` is PER REPOSITORY and assigned at
-- commit under the repository's own advisory lock (rows.go appendChange), so
-- two repositories never share, skip or collide on a cursor value.
-- caused_by is the seq that caused a function-authored write; NULL on every
-- direct write, so the causal-depth walk terminates.
--
-- `hash` is the entry's checksum over its stored bytes (checksum.go,
-- changelogfile.Encode). The length CHECK exists because the application role
-- holds UPDATE on this table: a malformed value must be impossible to store,
-- so the verifier only ever reasons about well-formed bytes.
--
-- `principal` is the verified token id behind the write — attribution the door
-- resolved, unlike the caller-asserted actor. It is hashed like any other
-- value, so it cannot be edited later without breaking the checksum.
--
-- `txn` is the seq of the appending transaction's LAST entry, stamped at
-- commit beside `hash` and carried by the segment line as `txn`
-- (changelogfile.Entry.Txn, line format 2), so the boot's table-to-file
-- catch-up appends whole transactions and never leaves a prefix of one in a
-- segment (repodir.go appendFromTable).
CREATE TABLE changelog (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    seq        bigint      NOT NULL,
    ts         timestamptz NOT NULL DEFAULT now(),
    actor      text        NOT NULL,
    op         text        NOT NULL,
    record_id  text        NOT NULL,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    caused_by  bigint,
    hash       bytea,
    principal  text        NOT NULL,
    txn        bigint,
    CONSTRAINT changelog_hash_len CHECK (hash IS NULL OR octet_length(hash) = 32),
    -- The cause is always an EARLIER entry (docs/changelog.md promises it);
    -- pinning it here also keeps zero out, so the preimage's NULL/value
    -- distinction (chain.go frameOptionalInt64) never meets a stored zero.
    CONSTRAINT changelog_caused_by_prior
        CHECK (caused_by IS NULL OR (caused_by >= 1 AND caused_by < seq)),
    -- `txn` ends the transaction the entry belongs to, so it is never below
    -- the entry's own seq.
    CONSTRAINT changelog_txn_ends_at_or_after_seq CHECK (txn IS NULL OR txn >= seq),
    PRIMARY KEY (repository, seq)
);
CREATE INDEX changelog_record_idx ON changelog (repository, record_id);
CREATE INDEX changelog_kind_idx   ON changelog (repository, kind);

-- The record-scoped change feed matches a merge or split entry on the pair its
-- payload names, not only on the row's own record_id (engine/changefeed.go): a
-- merge is addressed to the winner and tombstones the loser, a split is
-- addressed to the loser and rewrites the winner. `changelog_record_idx`
-- answers the record_id arm; without this index, the two payload arms make
-- every record-scoped read (the console's activity rail, GraphQL history, a
-- resumed record watch) walk the repository's whole changelog.
--
-- Partial on the two ops, so it holds one entry per merge or split and nothing
-- for the ordinary writes that are the bulk of a changelog: each payload arm
-- becomes a scan of the repository's merge and split rows, and the `->>` test
-- runs on those alone. Not an expression index on `payload->>'winner'`: the
-- changelog is under row-level security and `->>` is not leakproof, so the
-- planner may not evaluate it before the policy's repository qual and would
-- never probe such an index, only filter after it.
--
-- The reader spells `op IN ('merge', 'split')` as a literal, not a bound
-- parameter: the planner proves a partial index applicable only from a
-- predicate it can see, and a generic plan sees a parameter as any value.
CREATE INDEX changelog_pair_idx
    ON changelog (repository, seq) WHERE op IN ('merge', 'split');

-- Every stored vector names what bought it: `provider` is the llmprovider row
-- id and `model` the model id as sent. Cosine distance between two models'
-- vectors is not a distance, so the semantic query scores only vectors whose
-- pair is the repository's currently resolved one, and a half-finished
-- re-embed is a smaller result set rather than a mixed one. The empty string
-- resolves to no provider, so such a vector is invisible to search and
-- `reembed` is what replaces it.
--
-- No index rides with the pair. Both readers are keyed by record already: the
-- re-embed probe asks whether one (record, property) has a vector from the
-- resolved pair, and the semantic query filters the pair inside a join on
-- record_kind/record_id, so the primary key's leading columns serve both.
CREATE TABLE embeddings (
    repository  text NOT NULL DEFAULT current_setting('substrate.repository'),
    record_kind text NOT NULL,
    record_id   text NOT NULL,
    property    text NOT NULL,
    chunk       int  NOT NULL,
    text_hash   text NOT NULL,
    vec         public.vector(1536),
    provider    text NOT NULL DEFAULT '',
    model       text NOT NULL DEFAULT '',
    PRIMARY KEY (repository, record_kind, record_id, property, chunk)
);

-- `generation` is the source-text fence. A property's queue row is one per
-- (repository, record, property); an edit DURING a slow embed re-enqueues and
-- INCREMENTS the generation, so the worker — which snapshots the generation
-- before it embeds — can tell, when it comes to write, whether the text it
-- embedded is still the current text. It writes the vectors and drops the row
-- only while the generation it snapshotted still stands; a bumped generation
-- leaves the newer job pending and the stale vectors uncommitted.
CREATE TABLE embed_queue (
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
CREATE TABLE trigger_cursors (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    seq        bigint      NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (repository, trigger_id)
);

-- A parked failure's `id` is the seq of the changelog entry that parked it
-- (delivery.go, decision 0064), so a repository directory imported into an
-- empty database folds its failures back under the ids the retry API already
-- handed out. A seq is unique per repository, not per database, so the key is
-- (repository, id) and nothing allocates ids from a sequence.
--
-- `payload` is the delivery envelope a parked WEBHOOK fire retries with: that
-- fire has no changelog row underneath it, so the request it arrived with is
-- kept on the failure itself. NULL for record and schedule parks, whose
-- envelopes are rebuilt from the changelog or the clock.
CREATE TABLE trigger_failures (
    id         bigint      NOT NULL,
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    seq        bigint      NOT NULL,
    record_id  text        NOT NULL,
    attempts   integer     NOT NULL,
    last_error text        NOT NULL,
    parked_at  timestamptz NOT NULL,
    fire_id    text        NOT NULL DEFAULT '',
    payload    jsonb,
    PRIMARY KEY (repository, id)
);
CREATE INDEX trigger_failures_trigger ON trigger_failures (repository, trigger_id, parked_at);

CREATE TABLE trigger_schedule (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    trigger_id text        NOT NULL,
    fired_at   timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (repository, trigger_id)
);

-- The sealed store: secret material wrapped under the repository's DEK.
-- Records carry secret-typed REFS into this table, never raw tokens; nothing
-- here is ever in the changelog or in the fold's data. expires_at is denormalized
-- off the payload so the refresh loop queries cheaply.
CREATE TABLE sealed (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    ref         text        NOT NULL,
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    payload     bytea       NOT NULL,
    expires_at  timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, ref)
);
CREATE INDEX sealed_record ON sealed (repository, record_kind, record_id);
CREATE INDEX sealed_expiry ON sealed (repository, expires_at);

-- Pending OAuth flows: one row per started connect flow, keyed by the sha256
-- of the state's random nonce. The callback consumes its row atomically
-- (DELETE … RETURNING), which is what makes a signed state one-time.
CREATE TABLE oauth_flows (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    nonce_hash  text        NOT NULL,
    record_kind text        NOT NULL,
    record_id   text        NOT NULL,
    verifier    bytea       NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository, nonce_hash)
);
CREATE INDEX oauth_flows_expiry ON oauth_flows (repository, expires_at);

-- Paged-checkpoint invocation: a delivery whose body returns "more work + a
-- resume cursor" is re-invoked off the causal chain until drained, each page's
-- effects committed with the cursor. `version` is the cursor-ownership fence;
-- `effects`/`bytes`/`started_at` are the cumulative drain budget;
-- `trigger_id`/`kind`/`identity` give the row a lifecycle owner.
CREATE TABLE paged_cursors (
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
CREATE INDEX paged_cursors_trigger ON paged_cursors (repository, trigger_id);

-- The stored-declaration dialect: one row per repository, a monotonic integer
-- naming the shape of the declaration rows the repository holds, stamped by
-- the binary when the repository opens (dialect.go). A binary whose maximum is
-- below the stored value refuses the open rather than misreading rows it does
-- not understand. There is nothing to promote and no ledger of promotions: no
-- store predates this binary, so the stamp is a shape and never a count of
-- steps run.
CREATE TABLE vocabulary_dialect (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    dialect    integer     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- The changelog dialect: one row per repository, a monotonic integer naming
-- the spelling its changelog entries are written in, the ops and fold effects
-- a binary must understand to replay them. Where `vocabulary_dialect` governs
-- stored declarations, this stamp governs HISTORY, so a binary that cannot
-- replay a repository's changelog refuses the open instead of serving it until
-- somebody runs a rebuild and discovers the entry it cannot fold.
--
-- The row is written by the transaction that appends the entries it claims,
-- never by an open on its own: a claim over history a binary has not written
-- would bar an older binary for nothing.
--
-- Nothing ever rewrites what this stamp describes: a changelog is append-only
-- and its old entries keep their old spelling forever, so the stamp only ever
-- states what a replayer must understand.
CREATE TABLE changelog_dialect (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    dialect    integer     NOT NULL CHECK (dialect >= 1),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- The import-progress marker: one row per repository whose boot import has
-- begun and whose fold has not been rebuilt from the imported entries yet
-- (repodir.go importEntries). The row is written before the first batch of
-- changelog rows commits and deleted in the transaction that commits the last
-- fold pass, so a crash anywhere between leaves the row behind and the next
-- boot resumes the import instead of serving the fold as it stands. While
-- the row exists no dataset opens on the repository (ErrImportIncomplete).
--
-- The changelog head alone cannot carry this: the rows land in bounded
-- batches and the fold runs after the last one, so a crash after that batch
-- leaves the file head equal to the table head with nothing folded, which
-- reads as a repository that is up to date and empty.
--
-- `file_head` is the head the import is bringing the table to, for the boot
-- log and the refusal's message; the resume itself reads the table head.
CREATE TABLE import_progress (
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    file_head  bigint      NOT NULL CHECK (file_head >= 0),
    started_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository)
);

-- The idempotency key table behind the `Idempotency-Key` request header
-- (idempotency.go, docs/api.md "Idempotency and retries"): one row per
-- (repository, operation, key), so a retried create, function call, agent
-- call, merge or split answers the first attempt's outcome instead of running
-- again. The row for a one-transaction operation commits in the transaction
-- that commits the effect; a callable reserves its row before the body runs
-- (settled_at NULL) and settles it in the transaction that applies the
-- effects, so a concurrent retry sees the reservation and is refused.
--
-- The key binds to the repository and the operation, never to the token that
-- carried it: no token column, so a retry after logout and login matches.
-- `fingerprint` is the SHA-256 of the request's input; the same key with a
-- different input is refused. `outcome` is the stored answer as the engine
-- marshaled it, unparsed bytes rather than jsonb: nothing queries inside it,
-- and jsonb refuses a \u0000 escape, which would fail the settle after the
-- effects in the same transaction. NULL while the request is in flight and
-- NULL after settlement when the answer exceeded the retention cap (the
-- effect still ran once, and the retry says so; `locator` then names the
-- record a create, merge or split wrote, so the retry can read it). `owner`
-- is the attempt that holds the row, a random token: a stale attempt whose lease lapsed can
-- neither release nor overwrite a successor's row. `thread` is the agent
-- thread an agent call opened, written in the transaction that creates the
-- thread: the loop's tool effects commit before the thread settles, so a
-- reservation that names a thread is never released, and a retry is pointed
-- at the thread instead.
--
-- The table is Postgres-only bookkeeping and never enters the changelog: a
-- repository restored from its directory alone forgets every key, which
-- docs/api.md states. `expires_at` is the row's life: the retention window
-- once settled, the lease while in flight. Every read holds a row to it, so a
-- row past it is dead whether or not the GC sweep (gc.go) has reclaimed the
-- space, and the next attempt takes it over. A reservation without a thread
-- is also dead at boot, because one process writes a repository at a time.
CREATE TABLE idempotency_keys (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    operation   text        NOT NULL,
    key         text        NOT NULL,
    fingerprint text        NOT NULL,
    outcome     bytea,
    owner       text        NOT NULL,
    thread      text,
    locator     text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    settled_at  timestamptz,
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, operation, key)
);
CREATE INDEX idempotency_keys_expires ON idempotency_keys (repository, expires_at);

-- Row level security: ENABLE plus FORCE (so the owner is bound too), one FOR
-- ALL policy per repository-scoped table. The missing_ok form of
-- current_setting means an unscoped session matches nothing instead of
-- erroring — it fails closed. The array is every table carrying a
-- `repository` column, the same set engine/repositories.go
-- repositoryScopedTables names: a table added here without a policy is a
-- table any repository can read.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'records', 'refs', 'former_ids', 'annotations', 'property_managers',
        'property_offers', 'changelog', 'embeddings', 'embed_queue',
        'trigger_cursors', 'trigger_failures', 'trigger_schedule', 'sealed',
        'oauth_flows', 'paged_cursors', 'vocabulary_dialect',
        'changelog_dialect', 'import_progress', 'idempotency_keys'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR ALL'
            || ' USING (repository = current_setting(''substrate.repository'', true))'
            || ' WITH CHECK (repository = current_setting(''substrate.repository'', true))',
            t || '_repository', t);
    END LOOP;
END $$;

-- Grants, schema-local so parallel test schemas in one cluster do not fight.
-- substrate_app gets the repository-scoped tables and nothing else: the
-- control-plane table and the DDL ledger are maint's alone. The app role does
-- not DELETE a changelog dialect stamp (erasing a repository runs on the maint
-- pool), so that one table is granted three verbs, not four.
--
-- `ON ALL TABLES` reads the catalog at this point in the file, so every table
-- above is covered and a table a later migration adds carries its own grants.
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
        EXECUTE format('REVOKE DELETE ON TABLE %I.changelog_dialect FROM substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO substrate_maint', sch);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO substrate_maint', sch);
        EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO substrate_maint', sch);
    END IF;
END $$;
