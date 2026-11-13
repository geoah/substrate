package substrate

// RepositoryInfo describes one repository as the control-plane table holds it:
// its id and the lifecycle state. There is no schema name — every repository
// lives in the one shared schema.
type RepositoryInfo struct {
	// ID is the repository's id (decision 0261), minted at registration and
	// never changed: the row's primary key and the key of its storage.
	// Authority is its name: what its user registered and logs in with, and
	// the home of every kind that user declares.
	ID        string `json:"id"`
	Authority string `json:"authority"`
	State     string `json:"state"` // lifecycle machine state
}

// Connections are accountconfig-trait records and the sole install path is
// the vocabulary apply batch verb (a bundle closure): there is no connector
// registration kind and no POST …/connectors door.
