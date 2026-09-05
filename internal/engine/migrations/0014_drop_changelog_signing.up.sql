-- Changelog signing and the hash chain are retired: every entry now carries a
-- per-entry checksum in `hash` (checksum.go, changelogfile.Encode) and nothing
-- signs it. The signature column, its constraints, the chain-epoch table and
-- the per-repository signing state go; `hash` and `changelog_hash_len` stay,
-- because the column now holds the checksum. Idempotent: a database that has
-- already lost any of these passes through.
ALTER TABLE changelog
    DROP CONSTRAINT IF EXISTS changelog_sig_needs_hash,
    DROP CONSTRAINT IF EXISTS changelog_sig_len,
    DROP COLUMN IF EXISTS sig;

DROP TABLE IF EXISTS chain_epochs;

ALTER TABLE repositories
    DROP CONSTRAINT IF EXISTS repositories_signing_public_len,
    DROP CONSTRAINT IF EXISTS repositories_signing_whole,
    DROP CONSTRAINT IF EXISTS repositories_signed_from_positive,
    DROP COLUMN IF EXISTS signing_key,
    DROP COLUMN IF EXISTS signing_public,
    DROP COLUMN IF EXISTS signed_from_seq;
