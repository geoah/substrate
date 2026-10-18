\timing on
SET jit=off; SET parallel_setup_cost=0; SET parallel_tuple_cost=0;
\echo --- b UTC day parallel
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT date_trunc('day', ts) d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1;
\echo --- b Athens day parallel
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT date_trunc('day', ts, 'Europe/Athens') d, avg(value) FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1;
\echo --- a Athens day parallel
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT date_trunc('day', at, 'Europe/Athens') d, avg((props->>'bpm')::int) FROM records_a WHERE repository='alice.example.com' AND kind='alice.example.com/health/heartrate' AND at >= '2025-01-01' AND at < '2026-01-01' GROUP BY 1;
\echo --- c Athens day parallel
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT time_bucket('1 day', ts, 'Europe/Athens') d, avg(value) FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1;
\echo --- c: UTC-hour buckets then re-bucket hours into Athens days (two-stage)
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT date_trunc('day', h, 'Europe/Athens') d, sum(s)/sum(n) FROM (SELECT time_bucket('1 hour', ts) h, sum(value) s, count(*) n FROM samples_c WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) x GROUP BY 1;
\echo --- b: same two-stage
EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY ON) SELECT date_trunc('day', h, 'Europe/Athens') d, sum(s)/sum(n) FROM (SELECT date_trunc('hour', ts) h, sum(value) s, count(*) n FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-01-01' AND ts < '2026-01-01' GROUP BY 1) x GROUP BY 1;
