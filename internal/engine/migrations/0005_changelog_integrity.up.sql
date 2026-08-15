-- Changelog integrity: every entry carries a SHA-256 hash chaining to the
-- previous entry's (chain.go) and an Ed25519 signature over that hash
-- (signing.go). Both are stamped by the writing transaction; NULL hash marks
-- a pre-chain entry awaiting the first-open backfill and nothing else.
-- The length CHECKs exist because the application role holds UPDATE on this
-- table: a malformed value must be impossible to store, so the verifier only
-- ever reasons about well-formed bytes.
--
-- sig and principal are NOT NULL, and history written before them gets a
-- PLACEHOLDER, not an absence: the all-zero signature (an append-only log
-- cannot be signed after the fact; entries before signed_from_seq keep it
-- forever) and the principal 'invalid' (the token id was discarded before it
-- was ever stored, #102). Both placeholders are hashed like any value, so
-- neither can be edited later without breaking the chain. The DEFAULTs exist
-- only to stamp the rows this migration finds, and are dropped: the write
-- path supplies both explicitly. Pre-v1 scaffolding, tracked in #175.
-- principal itself is the verified token id behind the write — attribution
-- the door resolved, unlike the caller-asserted actor. Nothing stamps a real
-- one yet (#102); the column exists now because the chain hashes it from
-- birth, which is what lets #102 land without a preimage fork.
ALTER TABLE changelog
    ADD COLUMN IF NOT EXISTS hash bytea,
    ADD COLUMN IF NOT EXISTS sig bytea NOT NULL DEFAULT decode(repeat('00', 64), 'hex'),
    ADD COLUMN IF NOT EXISTS principal text NOT NULL DEFAULT 'invalid';
ALTER TABLE changelog
    ALTER COLUMN sig DROP DEFAULT,
    ALTER COLUMN principal DROP DEFAULT;
ALTER TABLE changelog
    ADD CONSTRAINT changelog_hash_len CHECK (hash IS NULL OR octet_length(hash) = 32),
    ADD CONSTRAINT changelog_sig_len CHECK (octet_length(sig) = 64),
    -- A real signature signs the hash, so it cannot exist without one; only
    -- the all-zero placeholder may sit on an unhashed (pre-backfill) row.
    ADD CONSTRAINT changelog_sig_needs_hash CHECK (hash IS NOT NULL OR sig = decode(repeat('00', 64), 'hex'));
-- The cause is always an earlier entry (docs/changelog.md promises it);
-- pinning it here also keeps zero out, so the preimage's NULL/value
-- distinction (chain.go frameOptionalInt64) never meets a stored zero.
-- NOT VALID on purpose: it binds every row written from here on without
-- scanning (or failing the boot over) history an older release wrote — a
-- historical violation is `repository verify`'s to report, not the
-- migration's to brick the server over.
ALTER TABLE changelog
    ADD CONSTRAINT changelog_caused_by_prior CHECK (caused_by IS NULL OR (caused_by >= 1 AND caused_by < seq)) NOT VALID;

-- chain_epochs records every sanctioned transition of a repository's chain:
-- the backfill that started attested history, a reseal's rewrite (which
-- re-chains everything after it), and signing activation. A re-chained
-- history is byte-indistinguishable from a tampered one; the epoch is what
-- lets a verifier explain a remembered head that no longer matches. It is
-- repository-scoped — it lives in the user plane with the history it
-- describes, and the reseal transaction (application pool, RLS-bound) must
-- write it atomically with the rewrite.
CREATE TABLE IF NOT EXISTS chain_epochs (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repository text        NOT NULL DEFAULT current_setting('substrate.repository'),
    at         timestamptz NOT NULL,
    reason     text        NOT NULL CHECK (reason IN ('backfill', 'reseal', 'activate')),
    from_seq   bigint      NOT NULL CHECK (from_seq >= 1),
    old_head   bytea       CHECK (old_head IS NULL OR octet_length(old_head) = 32),
    new_head   bytea       CHECK (new_head IS NULL OR octet_length(new_head) = 32),
    public_key bytea       CHECK (public_key IS NULL OR octet_length(public_key) = 32),
    signed_from bigint     CHECK (signed_from IS NULL OR signed_from >= 1),
    sig        bytea       CHECK (sig IS NULL OR octet_length(sig) = 64),
    -- An activation is signed by construction (it mints the key it signs
    -- with), so a bare `activate` row is a forgery the store itself refuses.
    CONSTRAINT chain_epochs_activate_whole CHECK (
        reason <> 'activate'
        OR (public_key IS NOT NULL AND signed_from IS NOT NULL AND sig IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS chain_epochs_repo ON chain_epochs (repository, id);

-- The same row level security every repository-scoped table gets (0001), for
-- the one new table.
ALTER TABLE chain_epochs ENABLE ROW LEVEL SECURITY;
ALTER TABLE chain_epochs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS chain_epochs_repository ON chain_epochs;
CREATE POLICY chain_epochs_repository ON chain_epochs FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

DO $$
DECLARE
    sch text := current_schema();
    seqname text := pg_get_serial_sequence(format('%I.chain_epochs', current_schema()), 'id');
BEGIN
    -- The application role INSERTS and SELECTS epochs and nothing else: an
    -- epoch is transition evidence, and the role that writes user data must
    -- not be able to rewrite or erase it. Erasure runs on the maint pool.
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT ON TABLE %I.chain_epochs TO substrate_app', sch);
        EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %s TO substrate_app', seqname);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.chain_epochs TO substrate_maint', sch);
        EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %s TO substrate_maint', seqname);
    END IF;
END $$;

-- Per-repository signing state, control plane. signing_key is the Ed25519
-- seed wrapped under the host credential key — SEALED framing only, never
-- plain (signing.go refuses a keyless host): the signature exists precisely
-- to resist a database-only attacker, so the key material must not sit
-- beside the signatures it mints. signed_from_seq is the durable, one-way
-- activation mark: from that seq on, an unsigned entry is a verification
-- failure and the engine refuses to append one.
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS signing_key bytea,
    ADD COLUMN IF NOT EXISTS signing_public bytea,
    ADD COLUMN IF NOT EXISTS signed_from_seq bigint;
ALTER TABLE repositories
    ADD CONSTRAINT repositories_signing_public_len
        CHECK (signing_public IS NULL OR octet_length(signing_public) = 32),
    ADD CONSTRAINT repositories_signing_whole
        CHECK ((signing_key IS NULL) = (signing_public IS NULL)
           AND (signing_key IS NULL) = (signed_from_seq IS NULL));
