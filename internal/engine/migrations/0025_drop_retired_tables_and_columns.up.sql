-- Two tables no code reads any more.
--
-- vocabulary_promotions recorded one completed step of the stored-schema
-- dialect ladder. The ladder is gone: no store predates this binary, so there
-- is nothing to promote, and the dialect stamp alone says what shape a store
-- is in (dialect.go).
--
-- repositories.sealed_dek_only marked a repository whose sealed store held no
-- plain and no host-key-sealed payload. Every repository is born that way and
-- nothing writes the other forms, so the column said the same thing about
-- every row (decision 0059).
--
-- blobs held blob bytes in a bytea column, the byte store before the data
-- root existed. The fs and s3 backends are the only ones there are, and the
-- blob manifest is a record, so the column had no reader left.
DROP TABLE IF EXISTS vocabulary_promotions;
DROP TABLE IF EXISTS blobs;
ALTER TABLE repositories DROP COLUMN IF EXISTS sealed_dek_only;
