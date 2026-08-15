-- Changelog integrity: every entry carries a SHA-256 hash chaining to the
-- previous entry's (chain.go), and optionally an Ed25519 signature over that
-- hash (signing.go). Both are stamped by the writing transaction; NULL hash
-- marks a pre-chain entry awaiting the first-open backfill and nothing else.
-- The length CHECKs exist because the application role holds UPDATE on this
-- table: a malformed value must be impossible to store, so the verifier only
-- ever reasons about well-formed bytes.
ALTER TABLE changelog
    ADD COLUMN IF NOT EXISTS hash bytea,
    ADD COLUMN IF NOT EXISTS sig  bytea;
ALTER TABLE changelog
    ADD CONSTRAINT changelog_hash_len CHECK (hash IS NULL OR octet_length(hash) = 32),
    ADD CONSTRAINT changelog_sig_len CHECK (sig IS NULL OR octet_length(sig) = 64),
    -- A signature signs the hash, so it cannot exist without one.
    ADD CONSTRAINT changelog_sig_needs_hash CHECK (sig IS NULL OR hash IS NOT NULL),
    -- The cause is always an earlier entry (docs/changelog.md promises it);
    -- pinning it here also keeps zero out, so the preimage's NULL/value
    -- distinction (chain.go frameOptionalInt64) never meets a stored zero.
    ADD CONSTRAINT changelog_caused_by_prior CHECK (caused_by IS NULL OR (caused_by >= 1 AND caused_by < seq));

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
    from_seq   bigint      NOT NULL,
    old_head   bytea       CHECK (old_head IS NULL OR octet_length(old_head) = 32),
    new_head   bytea       CHECK (new_head IS NULL OR octet_length(new_head) = 32),
    public_key bytea       CHECK (public_key IS NULL OR octet_length(public_key) = 32),
    signed_from bigint,
    sig        bytea       CHECK (sig IS NULL OR octet_length(sig) = 64)
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
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.chain_epochs TO substrate_app', sch);
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
