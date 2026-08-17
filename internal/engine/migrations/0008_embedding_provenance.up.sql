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
-- No index rides with this. Both readers are keyed by record already: the
-- re-embed probe asks whether one (record, property) has a vector from the
-- resolved pair, and the semantic query filters the pair inside a join on
-- record_kind/record_id, so the primary key's leading columns serve both. An
-- index on (repository, provider, model) would be a non-concurrent build
-- inside this transaction, blocking embeddings writes on a large table for the
-- length of it, in exchange for nothing either query asks for.
ALTER TABLE embeddings
    ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model    text NOT NULL DEFAULT '';
