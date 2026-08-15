-- The manager ledger records the PRINCIPAL beside the actor: the token id the
-- door verified for the write, where the actor is only what the caller
-- asserted (#102). The changelog carries the same value on every entry
-- (0005), and this table is that changelog's fold, so a rebuild reproduces
-- these rows from the effects the entries carry.
--
-- Empty is the one spelling for "no principal on this row": no token stood
-- behind the write (the seed, the boot upgrade, a background worker,
-- registration and login), or the row predates this column. The changelog
-- keeps the two apart -- it is
-- history and cannot be rewritten, so its pre-#102 entries keep the
-- 'invalid' placeholder forever -- while a fold row is replaced wholesale by
-- the next replay, and one "unknown" is enough for it. The DEFAULT exists
-- only to stamp the rows this migration finds, and is dropped: the write
-- path supplies the value explicitly.
ALTER TABLE property_managers
    ADD COLUMN IF NOT EXISTS principal text NOT NULL DEFAULT '';
ALTER TABLE property_managers
    ALTER COLUMN principal DROP DEFAULT;
