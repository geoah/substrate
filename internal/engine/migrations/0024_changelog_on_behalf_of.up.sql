-- A write records three links (decision 0270): the actor record that wrote
-- it, the one it acted for, and the token behind it. `actor` holds the first
-- and `principal` the third, as ids; this column holds the second, empty
-- where the actor acted for no one. It changes the schema alone: a line
-- written before the links is rewritten by the changelog conversion, and the
-- table takes the converted lines with it (engine/conversion.go).
ALTER TABLE changelog ADD COLUMN IF NOT EXISTS on_behalf_of text NOT NULL DEFAULT '';
