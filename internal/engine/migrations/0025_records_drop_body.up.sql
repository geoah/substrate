-- A record's `body` is an ordinary property (decision 0273): its value lives
-- in `props` beside the record's other properties, and from changelog dialect
-- 4 its history carries it there too (conversion_0004). The column goes, and
-- no row is written: every repository's history is converted and its records
-- rebuilt from the converted lines at the boot check, before anything folds
-- or serves it, so each value this drops is folded back into `props`.
ALTER TABLE records DROP COLUMN IF EXISTS body;
