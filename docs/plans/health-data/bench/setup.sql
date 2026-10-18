\timing on
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS vector;
SET timezone='UTC';
-- source sample stream: one year of heart rate every 5 s
CREATE UNLOGGED TABLE src AS
SELECT ts, (60 + 15*sin(extract(epoch from ts)/3600.0) + (random()*10))::int AS bpm
FROM generate_series('2025-01-01 00:00:00+00'::timestamptz, '2025-12-31 23:59:55+00', '5 seconds') ts;
SELECT count(*) FROM src;

-- (a) substrate's records table, one record per sample
CREATE TABLE records_a (
    repository text NOT NULL, kind text NOT NULL, id text NOT NULL,
    title text NOT NULL DEFAULT '', body text NOT NULL DEFAULT '',
    states jsonb NOT NULL DEFAULT '{}', at timestamptz, ends_at timestamptz, due_at timestamptz,
    props jsonb NOT NULL DEFAULT '{}', labels jsonb NOT NULL DEFAULT '{}',
    version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz,
    finalizers text[] NOT NULL DEFAULT '{}', fts tsvector, kind_version bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (repository, kind, id));
CREATE INDEX ON records_a (repository, at);
CREATE INDEX ON records_a (repository, ends_at);
CREATE INDEX ON records_a (repository, due_at);
CREATE INDEX ON records_a (repository, created_at);
CREATE INDEX ON records_a (repository, updated_at);
CREATE INDEX ON records_a (repository, deleted_at);
CREATE INDEX ON records_a USING gin (props jsonb_path_ops);
CREATE INDEX ON records_a USING gin (labels jsonb_path_ops);
CREATE INDEX ON records_a USING gin (states jsonb_path_ops);
CREATE INDEX ON records_a USING gin (fts);
INSERT INTO records_a (repository, kind, id, at, props, fts)
SELECT 'alice.example.com', 'alice.example.com/health/heartrate',
       md5(ts::text)::text, ts,
       jsonb_build_object('bpm', bpm, 'source', jsonb_build_object('ref','providers.substrate.reamde.dev/whoop/account/acct1')),
       ''::tsvector
FROM src;

-- (b) narrow samples table, btree PK
CREATE TABLE samples_b (repository text NOT NULL, series int NOT NULL, ts timestamptz NOT NULL, value float8 NOT NULL,
  PRIMARY KEY (repository, series, ts));
INSERT INTO samples_b SELECT 'alice.example.com', 1, ts, bpm FROM src;

-- (b2) narrow samples, BRIN only, monthly partitions
CREATE TABLE samples_b2 (repository text NOT NULL, series int NOT NULL, ts timestamptz NOT NULL, value float8 NOT NULL)
  PARTITION BY RANGE (ts);
DO $$ DECLARE m date; BEGIN
 FOR m IN SELECT generate_series('2025-01-01'::date,'2025-12-01','1 month') LOOP
  EXECUTE format('CREATE TABLE samples_b2_%s PARTITION OF samples_b2 FOR VALUES FROM (%L) TO (%L)', to_char(m,'YYYYMM'), m, m + interval '1 month');
 END LOOP; END $$;
CREATE INDEX ON samples_b2 USING brin (ts) WITH (pages_per_range=32);
INSERT INTO samples_b2 SELECT 'alice.example.com', 1, ts, bpm FROM src;

-- (c) Timescale hypertable, compressed
CREATE TABLE samples_c (repository text NOT NULL, series int NOT NULL, ts timestamptz NOT NULL, value float8 NOT NULL);
SELECT create_hypertable('samples_c', by_range('ts', interval '7 days'));
INSERT INTO samples_c SELECT 'alice.example.com', 1, ts, bpm FROM src;
SELECT pg_size_pretty(hypertable_size('samples_c')) AS c_before;
ALTER TABLE samples_c SET (timescaledb.compress, timescaledb.compress_segmentby = 'repository, series', timescaledb.compress_orderby = 'ts');
SELECT count(compress_chunk(c)) FROM show_chunks('samples_c') c;

-- (d1) chunked series: one record per local day, jsonb arrays (offset seconds, value) + summary stats
CREATE TABLE records_d (LIKE records_a INCLUDING DEFAULTS);
ALTER TABLE records_d ADD PRIMARY KEY (repository, kind, id);
CREATE INDEX ON records_d (repository, at);
CREATE INDEX ON records_d USING gin (props jsonb_path_ops);
INSERT INTO records_d (repository, kind, id, at, ends_at, props)
SELECT 'alice.example.com', 'alice.example.com/health/heartrateseries', to_char(d,'YYYY-MM-DD'), d, d + interval '1 day',
  jsonb_build_object(
    'source', jsonb_build_object('ref','providers.substrate.reamde.dev/whoop/account/acct1'),
    'unit','/min',
    'stats', jsonb_build_object('count',count(*),'sum',sum(bpm),'min',min(bpm),'max',max(bpm),'sumsq',sum(bpm::bigint*bpm)),
    't', jsonb_agg(extract(epoch from ts - d)::int ORDER BY ts),
    'v', jsonb_agg(bpm ORDER BY ts))
FROM (SELECT date_trunc('day', ts) d, ts, bpm FROM src) s GROUP BY d;

-- (d2) chunked series, binary: int4 offset array and int2 value array in bytea-ish native arrays
CREATE TABLE chunks_d2 (repository text, series int, day timestamptz, n int, sum float8, min float8, max float8, t int4[], v int2[],
  PRIMARY KEY (repository, series, day));
INSERT INTO chunks_d2 SELECT 'alice.example.com', 1, d, count(*), sum(bpm), min(bpm), max(bpm),
   array_agg(extract(epoch from ts - d)::int ORDER BY ts), array_agg(bpm::int2 ORDER BY ts)
FROM (SELECT date_trunc('day', ts) d, ts, bpm FROM src) s GROUP BY d;

VACUUM ANALYZE;
