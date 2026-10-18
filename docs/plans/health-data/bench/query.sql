\timing on
SET timezone='UTC';
SET jit = off;
\echo === sizes
SELECT relname, pg_size_pretty(pg_relation_size(oid)) heap, pg_size_pretty(pg_indexes_size(oid)) idx, pg_size_pretty(pg_total_relation_size(oid)) total, pg_total_relation_size(oid) bytes
FROM pg_class WHERE relname IN ('src','records_a','samples_b','records_d','chunks_d2') ORDER BY 1;
SELECT 'samples_b2' rel, pg_size_pretty(sum(pg_total_relation_size(inhrelid))) total, sum(pg_total_relation_size(inhrelid)) bytes FROM pg_inherits WHERE inhparent='samples_b2'::regclass;
SELECT 'samples_c' rel, pg_size_pretty(hypertable_size('samples_c')) total, hypertable_size('samples_c') bytes;
SELECT * FROM hypertable_compression_stats('samples_c');
SELECT avg(pg_column_size(props)) a_props_bytes FROM records_a;
SELECT avg(pg_column_size(props)) d_props_bytes_per_day FROM records_d;
SELECT avg(pg_column_size(t)+pg_column_size(v)) d2_arrays_bytes_per_day FROM chunks_d2;
\echo === Q: avg bpm per local day (Europe/Athens), 2025, each option, run twice (second is warm)
\echo --- a records jsonb
SELECT count(*) FROM (SELECT date_trunc('day', at, 'Europe/Athens') d, avg((props->>'bpm')::int) FROM records_a WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrate' AND at >= '2025-01-01' AND at < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', at, 'Europe/Athens') d, avg((props->>'bpm')::int) FROM records_a WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrate' AND at >= '2025-01-01' AND at < '2026-01-01' GROUP BY 1) q;
\echo --- b narrow btree
SELECT count(*) FROM (SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- b2 narrow brin partitioned
SELECT count(*) FROM (SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b2 WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b2 WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- c timescale compressed
SELECT count(*) FROM (SELECT time_bucket('1 day', ts, 'Europe/Athens') d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT time_bucket('1 day', ts, 'Europe/Athens') d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- d1 jsonb chunk, precomputed stats only (UTC days, as chunked)
SELECT count(*) FROM (SELECT at, (props->'stats'->>'sum')::float8/(props->'stats'->>'count')::float8 FROM records_d WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrateseries' AND at >= '2025-01-01' AND at < '2026-01-01') q;
SELECT count(*) FROM (SELECT at, (props->'stats'->>'sum')::float8/(props->'stats'->>'count')::float8 FROM records_d WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrateseries' AND at >= '2025-01-01' AND at < '2026-01-01') q;
\echo --- d1 jsonb chunk, unpacked and rebucketed by local day
SELECT count(*) FROM (SELECT date_trunc('day', r.at + make_interval(secs => t.x::int), 'Europe/Athens') d, avg(v.y::int)
 FROM records_d r, LATERAL jsonb_array_elements_text(r.props->'t') WITH ORDINALITY t(x,i) JOIN LATERAL jsonb_array_elements_text(r.props->'v') WITH ORDINALITY v(y,j) ON i=j
 WHERE r.repository='alice.example.com' AND r.kind='alice.example.com/health/heartrateseries' AND r.at >= '2025-01-01' AND r.at < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', r.at + make_interval(secs => t.x::int), 'Europe/Athens') d, avg(v.y::int)
 FROM records_d r, LATERAL jsonb_array_elements_text(r.props->'t') WITH ORDINALITY t(x,i) JOIN LATERAL jsonb_array_elements_text(r.props->'v') WITH ORDINALITY v(y,j) ON i=j
 WHERE r.repository='alice.example.com' AND r.kind='alice.example.com/health/heartrateseries' AND r.at >= '2025-01-01' AND r.at < '2026-01-01' GROUP BY 1) q;
\echo --- d2 native arrays, unnest and rebucket by local day
SELECT count(*) FROM (SELECT date_trunc('day', c.day + make_interval(secs => u.t), 'Europe/Athens') d, avg(u.v)
 FROM chunks_d2 c, LATERAL unnest(c.t, c.v) u(t,v) WHERE c.repository='alice.example.com' AND c.series=1 AND c.day >= '2025-01-01' AND c.day < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', c.day + make_interval(secs => u.t), 'Europe/Athens') d, avg(u.v)
 FROM chunks_d2 c, LATERAL unnest(c.t, c.v) u(t,v) WHERE c.repository='alice.example.com' AND c.series=1 AND c.day >= '2025-01-01' AND c.day < '2026-01-01' GROUP BY 1) q;
\echo --- one day raw read for a chart (narrow vs chunk)
SELECT count(*) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-06-01' AND ts < '2025-06-02';
SELECT count(*) FROM chunks_d2 c, unnest(c.t,c.v) WHERE repository='alice.example.com' AND series=1 AND day='2025-06-01';
\echo --- correction of one sample
BEGIN; UPDATE samples_b SET value=99 WHERE repository='alice.example.com' AND series=1 AND ts='2025-06-01 12:00:00+00'; ROLLBACK;
BEGIN; UPDATE samples_c SET value=99 WHERE repository='alice.example.com' AND series=1 AND ts='2025-06-01 12:00:00+00'; ROLLBACK;
BEGIN; UPDATE chunks_d2 SET v[8641]=99 WHERE repository='alice.example.com' AND series=1 AND day='2025-06-01'; ROLLBACK;
\echo --- changelog-line proxies: text size of one chunk record's props, one sample record's props
SELECT avg(length(props::text)) chunk_props_text_bytes FROM records_d;
SELECT avg(length(props::text)) sample_props_text_bytes FROM records_a LIMIT 1;
\echo --- rebuild a derived narrow table from the chunk records (option e)
CREATE UNLOGGED TABLE derived_e (repository text, series int, ts timestamptz, value float8);
INSERT INTO derived_e SELECT c.repository, c.series, c.day + make_interval(secs => u.t), u.v FROM chunks_d2 c, unnest(c.t,c.v) u(t,v);
INSERT INTO derived_e SELECT r.repository, 2, r.at + make_interval(secs => t.x::int), v.y::int FROM records_d r, LATERAL jsonb_array_elements_text(r.props->'t') WITH ORDINALITY t(x,i) JOIN LATERAL jsonb_array_elements_text(r.props->'v') WITH ORDINALITY v(y,j) ON i=j;
CREATE INDEX ON derived_e (repository, series, ts);
\echo --- daily rollup table (a derived continuous aggregate by hand) and its query
CREATE TABLE rollup_day AS SELECT repository, series, date_trunc('day', ts, 'Europe/Athens') d, count(*) n, sum(value) s, min(value) mn, max(value) mx FROM samples_b GROUP BY 1,2,3;
SELECT count(*) FROM (SELECT d, s/n FROM rollup_day WHERE repository='alice.example.com' AND series=1 AND d >= '2025-01-01' AND d < '2026-01-01') q;
