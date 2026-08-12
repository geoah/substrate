package substrate

// RepositoryInfo describes one repository as the control-plane table holds it:
// the opaque internal id, the owning user's username, and the lifecycle state.
// There is no schema name — every repository lives in the one shared schema.
type RepositoryInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"` // lifecycle machine state
}

// The legacy connector-registration types (ConnectorManifest / ConnectorTrigger)
// and the POST …/connectors shim were REMOVED at the v1 freeze (ticket 004,
// ruling A12). Connections are accountconfig-trait records and the sole
// install path is the schema-apply batch verb (a bundle closure). The one
// remaining reader of the old on-disk manifest shape — the historical
// stored-manifest promotion (dialect step 4, a no-op on any v1 repository) — keeps
// its own unexported struct in the schema package; it is not a wire contract.
