-- A parked webhook fire must retry with the request it arrived with: the fire
-- has no changelog row underneath it, so the delivery envelope is kept on the
-- parked failure itself. NULL for record and schedule parks, whose envelopes
-- are rebuilt from the changelog or the clock.
ALTER TABLE trigger_failures ADD COLUMN IF NOT EXISTS payload jsonb;
