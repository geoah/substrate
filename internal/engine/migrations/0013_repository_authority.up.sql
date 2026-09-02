-- A repository owns one DNS-style authority: the home of every kind its user
-- declares, chosen at registration and permanent (decision record 0046). It
-- is unique across the substrate like the username, because a kind reference
-- names its authority and nothing else. A row from before this column is
-- given `<username>.localhost`, a legal authority no DNS resolves, so an
-- existing dev database still opens; a fresh registration always writes its
-- own.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS authority text;
UPDATE repositories SET authority = username || '.localhost' WHERE authority IS NULL;
ALTER TABLE repositories ALTER COLUMN authority SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS repositories_authority_key ON repositories (authority);
