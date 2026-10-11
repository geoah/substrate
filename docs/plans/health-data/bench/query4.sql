\timing on
SET jit=off;
CREATE TABLE rollup_15m AS SELECT repository, series, date_trunc('hour', ts) + floor(extract(minute from ts)/15)*interval '15 min' b, count(*)::int n, sum(value) s, min(value) mn, max(value) mx, sum(value*value) ss FROM samples_b GROUP BY 1,2,3;
ALTER TABLE rollup_15m ADD PRIMARY KEY (repository, series, b);
VACUUM ANALYZE rollup_15m;
SELECT pg_size_pretty(pg_total_relation_size('rollup_15m')), count(*) FROM rollup_15m;
\echo --- Athens day from 15m rollups, avg min max sd
SELECT count(*) FROM (SELECT date_trunc('day', b, 'Europe/Athens') d, sum(s)/sum(n) avg, min(mn), max(mx), sqrt((sum(ss)-sum(s)^2/sum(n))/(sum(n)-1)) sd FROM rollup_15m WHERE repository='alice.example.com' AND series=1 AND b >= '2025-01-01' AND b < '2026-01-01' GROUP BY 1) q;
SELECT count(*) FROM (SELECT date_trunc('day', b, 'Europe/Athens') d, sum(s)/sum(n) avg, min(mn), max(mx), sqrt((sum(ss)-sum(s)^2/sum(n))/(sum(n)-1)) sd FROM rollup_15m WHERE repository='alice.example.com' AND series=1 AND b >= '2025-01-01' AND b < '2026-01-01' GROUP BY 1) q;
\echo --- Kolkata (+05:30) day from 15m rollups
SELECT count(*) FROM (SELECT date_trunc('day', b, 'Asia/Kolkata') d, sum(s)/sum(n) FROM rollup_15m WHERE repository='alice.example.com' AND series=1 GROUP BY 1) q;
\echo --- rolling 7-day mean of daily avg over a year from rollups
SELECT count(*) FROM (SELECT d, avg(a) OVER (ORDER BY d ROWS 6 PRECEDING) FROM (SELECT date_trunc('day', b, 'Europe/Athens') d, sum(s)/sum(n) a FROM rollup_15m WHERE repository='alice.example.com' AND series=1 GROUP BY 1) x) q;
\echo --- rebuild 15m rollups from samples_b, timed (the derived-index rebuild cost)
CREATE TEMP TABLE r2 AS SELECT repository, series, date_bin('15 minutes', ts, '2000-01-01'::timestamptz) b, count(*)::int n, sum(value) s, min(value) mn, max(value) mx, sum(value*value) ss FROM samples_b GROUP BY 1,2,3;
\echo --- M4 downsampling one week to 800 columns from samples_b
SELECT count(*) FROM (SELECT width_bucket(extract(epoch from ts), extract(epoch from '2025-06-01'::timestamptz), extract(epoch from '2025-06-08'::timestamptz), 800) px, min(value), max(value), (array_agg(value ORDER BY ts))[1] first, (array_agg(value ORDER BY ts DESC))[1] last FROM samples_b WHERE repository='alice.example.com' AND series=1 AND ts >= '2025-06-01' AND ts < '2025-06-08' GROUP BY 1) q;
