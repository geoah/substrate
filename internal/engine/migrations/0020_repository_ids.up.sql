-- A repository's id is no longer its name (decision 0261): `id` is an opaque
-- key minted at creation (`rnd-…`), and `authority` is the name a person signs
-- in with, unique as before (repositories_authority_key). The constraint that
-- held the two equal goes. This changes the schema alone: the boot converts
-- each repository a release before ids left keyed by its name, rows and
-- directory together (engine/repositoryid.go), because the `repository`
-- column of a fold table is rewritten there and no SQL migration writes a
-- fold row.
ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_id_is_authority;
