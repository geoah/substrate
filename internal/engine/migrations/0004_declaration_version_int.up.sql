-- The declaration version is an incremental integer now: the Kubernetes-style
-- strings map to their trailing number (v1alpha3 -> 3), a plain digit string
-- to its number, and anything else a hand once wrote to 1 -- a version it had,
-- the first. Only the declaration kinds carry the property; a `version`
-- property some data kind declares is that kind's own business and stays.
--
-- The changelog is NOT rewritten (append-only, the truth): a rebuild refolds
-- the old strings into records.props, where they read as the absent version 0
-- and the next open's shipped-vocabulary upgrade rewrites them as numbers.
UPDATE records
SET props = jsonb_set(
    props,
    '{version}',
    to_jsonb(COALESCE(
        substring(props->>'version' FROM '^v1alpha([0-9]+)$')::bigint,
        substring(props->>'version' FROM '^([0-9]+)$')::bigint,
        1))
)
WHERE kind IN (
    'core.substrate.reamde.dev/authority',
    'core.substrate.reamde.dev/actor',
    'core.substrate.reamde.dev/kind',
    'core.substrate.reamde.dev/trait',
    'core.substrate.reamde.dev/propertytype',
    'core.substrate.reamde.dev/recordmapping',
    'core.substrate.reamde.dev/function',
    'core.substrate.reamde.dev/agent',
    'core.substrate.reamde.dev/bundle'
)
AND props ? 'version'
AND jsonb_typeof(props->'version') = 'string';
