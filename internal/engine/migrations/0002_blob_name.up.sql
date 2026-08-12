-- A blob carries a NAME beside its media type: the filename the bytes arrived
-- as, so an attachment reads as "invoice.pdf" and not as 76 characters of
-- sha-256. It is metadata, never identity — the digest is still the id, and
-- two uploads of the same bytes under different names are still ONE blob (the
-- first name wins, exactly as the first mime type does).
--
-- Both columns are optional by declaration: an empty string is "not said",
-- which is what a PUT with no Content-Type and no name stores.
ALTER TABLE blobs ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '';
