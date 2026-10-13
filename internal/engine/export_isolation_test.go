package engine

import "slices"

// RepositoryScopedTables is the list a repository's erase, its residue check
// and the boot's orphan sweep walk (repositories.go), so the isolation suite
// asserts row level security on exactly the tables those walk and holds the
// list to the schema.
func RepositoryScopedTables() []string {
	return slices.Clone(repositoryScopedTables)
}
