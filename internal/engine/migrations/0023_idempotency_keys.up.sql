-- The idempotency key table behind the `Idempotency-Key` request header
-- (engine idempotency.go, docs/api.md "Idempotency and retries"): one row per
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
-- different input is refused. `outcome` is the stored answer, NULL while the
-- request is in flight and NULL after settlement when the answer exceeded the
-- retention cap (the effect still ran once, and the retry says so).
--
-- The table is Postgres-only bookkeeping and never enters the changelog: a
-- repository restored from its directory alone forgets every key, which
-- docs/api.md states. `expires_at` is the retention window a GC sweep
-- deletes past (gc.go), and while a row is in flight it is the lease a stale
-- reservation can be taken over after.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    repository  text        NOT NULL DEFAULT current_setting('substrate.repository'),
    operation   text        NOT NULL,
    key         text        NOT NULL,
    fingerprint text        NOT NULL,
    outcome     jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    settled_at  timestamptz,
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (repository, operation, key)
);
CREATE INDEX IF NOT EXISTS idempotency_keys_expires ON idempotency_keys (repository, expires_at);

-- The same row level security every repository-scoped table gets (0001).
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS idempotency_keys_repository ON idempotency_keys;
CREATE POLICY idempotency_keys_repository ON idempotency_keys FOR ALL
    USING (repository = current_setting('substrate.repository', true))
    WITH CHECK (repository = current_setting('substrate.repository', true));

-- 0001 granted the tables that existed when it ran, so a table added later
-- carries its own grants. Every write and the sweep run on the application
-- pool; erasing a repository runs on the maint pool.
DO $$
DECLARE
    sch text := current_schema();
BEGIN
    IF to_regrole('substrate_app') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.idempotency_keys TO substrate_app', sch);
    END IF;
    IF to_regrole('substrate_maint') IS NOT NULL THEN
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.idempotency_keys TO substrate_maint', sch);
    END IF;
END $$;
