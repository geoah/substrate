-- Whether an import left this repository's secrets behind (decision 0264,
-- engine/lostsecrets.go). The import of a directory the operator listed in
-- SUBSTRATE_IMPORT_WITHOUT_SECRETS keeps every sealed value this host's key
-- does not open in the sealed store and sets it; the next open takes each
-- such value's reference out of the record that holds it, removes the value
-- in the same transaction, and lifts it. While it is set, a sealed value that
-- does not open is a loss the operator accepted; once it is lifted, one is
-- damage. It is the control plane's, as the data key's wrap beside it is.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS secrets_left_behind boolean NOT NULL DEFAULT false;
