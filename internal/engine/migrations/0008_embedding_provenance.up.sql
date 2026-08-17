-- Every stored vector names what bought it.
--
-- Before this, the embedder was one process-wide client built at boot from
-- SUBSTRATE_LLM_BASE_URL / SUBSTRATE_LLM_API_KEY / SUBSTRATE_LLM_EMBED_MODEL,
-- and nothing recorded which model produced a row. Change the model, or the
-- gateway behind the same model name, and the table held vectors from two
-- models at once. Cosine distance between two models' vectors is not a
-- distance, so search went wrong with no error anywhere.
--
-- `provider` is the llmprovider row id that bought the vector and `model` is
-- the model id as sent. The semantic query scores only vectors whose pair is
-- the repository's currently resolved one, so a half-finished re-embed is a
-- smaller result set and never a mixed one.
--
-- The empty string is what every already-stored vector gets. It resolves to no
-- provider, so those vectors are invisible to search from this migration
-- forward, and `reembed` is what replaces them.
ALTER TABLE embeddings
    ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model    text NOT NULL DEFAULT '';

-- The re-embed scan asks one question per repository: which stored vectors did
-- some other pair produce. Without this it reads every vector in the table.
CREATE INDEX IF NOT EXISTS embeddings_provenance_idx
    ON embeddings (repository, provider, model);
