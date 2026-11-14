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
	// answers to, newest first (decision record 0287): each is another
	// spelling of `~` on every input until it lapses. Not on the wire: a
	// reply names the repository by Authority, and RepositoryNames lists
	// them.
	FormerNames []string `json:"-"`
	// Spellings are the names that spell `~` on an input: Names but one a
	// package of the repository's own is published under (decision record
	// 0289). Nil where the server did not say, and then Names stand.
	Spellings []string `json:"-"`
}

// Names are the names the repository answers to: Authority, then its former
// names in force.
func (r RepositoryInfo) Names() []string {
	if r.Authority == "" {
		return nil
	}
	return append([]string{r.Authority}, r.FormerNames...)
}

// LocalNames are the names an input may spell `~` with: Spellings where the
// server set them, Names otherwise.
func (r RepositoryInfo) LocalNames() []string {
	if r.Spellings != nil {
		return r.Spellings
	}
	return r.Names()
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
