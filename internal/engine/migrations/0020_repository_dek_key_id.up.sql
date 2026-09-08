-- Two facts about a repository's keys that the row did not record (decision
-- record 0059).
--
-- dek_key_id names the SUBSTRATE_CREDENTIAL_KEY the `dek` bytes are wrapped
-- under: 16 hex digits of a one-way hash over the key material, never the key.
-- A host holding another key is told which key the wrap wants instead of "no
-- key opens it". NULL is a wrap written before the id was stored, or under no
-- key; the first open under a keyed host fills it in.
--
-- sealed_dek_only records that every payload in the repository's sealed store
-- is bound-framed ('a') under the repository's DEK: no plain ('p') payload and
-- none sealed under the host key remain. FALSE on every existing row, so the
-- next open runs the re-key pass over the store once and sets it; from then on
-- a read refuses those two forms rather than tolerating them.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS dek_key_id text;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS sealed_dek_only boolean NOT NULL DEFAULT false;
