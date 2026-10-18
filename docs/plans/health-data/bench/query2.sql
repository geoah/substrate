\timing on
SET jit=off;
\echo --- a jsonb, UTC day
SELECT count(*) FROM (SELECT date_trunc('day', at) d, avg((props->>'bpm')::int) FROM records_a WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrate' AND at >= '2025-01-01' AND at < '2026-01-01' GROUP BY 1) q;
\echo --- b narrow, UTC day
SELECT count(*) FROM (SELECT date_trunc('day', ts) d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', ts) d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- b narrow, stored-offset local day ((ts + offset) at UTC)::date with constant offset 180 min
SELECT count(*) FROM (SELECT ((ts + make_interval(mins => 180)) AT TIME ZONE 'UTC')::date d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- b narrow, epoch arithmetic day
SELECT count(*) FROM (SELECT floor((extract(epoch from ts)+10800)/86400) d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- c timescale, UTC day
SELECT count(*) FROM (SELECT time_bucket('1 day', ts) d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT time_bucket('1 day', ts) d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) q;
\echo --- d2 arrays, UTC day = chunk day, unnest
SELECT count(*) FROM (SELECT c.day, avg(u.v) FROM chunks_d2 c, unnest(c.v) u(v) WHERE c.repository='alice.example.com' AND c.series=1 GROUP BY 1) q;
\echo --- plain count/avg over the year, no bucketing: a, b, c
SELECT avg((props->>'bpm')::int) FROM records_a WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrate' AND at >= '2025-01-01' AND at < '2026-01-01';
SELECT avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01';
SELECT avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01';
\echo --- one month, Athens day, b and c
SELECT count(*) FROM (SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-06-01' AND ts < '2025-07-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT time_bucket('1 day', ts, 'Europe/Athens') d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-06-01' AND ts < '2025-07-01' GROUP BY 1) q;
\echo --- plan check b
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) SELECT date_trunc('day', ts) d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1;
