-- Each repository's data-encryption key (DEK), wrapped: the sealed store's
-- payloads encrypt under the repository's own random key, and this column
-- holds that key wrapped under the host credential key (the same plain/sealed
-- framing the store uses, so a keyless host stores it plain-marked, loudly).
-- The user plane never depends on the host key: the recoverykey record inside
-- the repository carries the same DEK wrapped to the user's age recipient.
-- NULL marks a repository from before DEKs; open adopts one lazily and the
-- reseal migration re-keys its payloads.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS dek bytea;
