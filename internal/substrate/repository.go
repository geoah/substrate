package substrate

// RepositoryInfo describes one repository as the control-plane table holds it:
// its id and the lifecycle state. There is no schema name — every repository
// lives in the one shared schema.
type RepositoryInfo struct {
	// ID is the repository's authority (decision 0052), the row's primary
	// key: the name its user registered and logs in with, and the home of
	// every kind that user declares. It always equals Authority, which stays
	// for the readers that ask for the name by that word.
	ID        string `json:"id"`
	Authority string `json:"authority"`
	State     string `json:"state"` // lifecycle machine state
}

// Connections are accountconfig-trait records and the sole install path is
// the schema-apply batch verb (a bundle closure): there is no connector
// registration type and no POST …/connectors door.
