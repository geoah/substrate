package substrate

import "time"

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
	// FormerNames are the names the repository had before a rename and still
	// answers to, newest first (decision record 0287), each until it lapses.
	// Not on the wire: a reply names the repository by Authority, and
	// RepositoryNames lists them.
	FormerNames []string `json:"-"`
	// Spellings are the names that spell `~` on an input: Authority and
	// FormerNames but a name a package the repository holds is published
	// under, which names that package's kinds instead. Nil where the server
	// did not say, and then every name the repository answers to does.
	Spellings []string `json:"-"`
}

// LocalNames are the names an input may spell `~` with: Spellings where the
// server set them, else every name the repository answers to. Every fold of
// a name reads these; there is no accessor for the names alone, so a fold
// cannot read them by mistake (TestEveryFoldReadsTheSpellings).
func (r RepositoryInfo) LocalNames() []string {
	if r.Spellings != nil {
		return r.Spellings
	}
	if r.Authority == "" {
		return nil
	}
	return append([]string{r.Authority}, r.FormerNames...)
}

// RepositoryNames is what a rename answers (POST /repository/rename): the
// repository's id, which no rename moves, the name it has now, and the
// former names it still answers to and holds against every other
// registration, each until the moment it lapses (decision record 0287).
type RepositoryNames struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	FormerNames []FormerName `json:"formerNames"`
}

// FormerName is one name a repository had before a rename, and when it stops
// resolving to it and is free for anyone.
type FormerName struct {
	Name  string    `json:"name"`
	Until time.Time `json:"until"`
}

// Connections are accountconfig-trait records and the sole install path is
// the vocabulary apply batch verb (a bundle closure): there is no connector
// registration kind and no POST …/connectors door.
