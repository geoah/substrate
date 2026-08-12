// Package substrate is the contract of the substrate: the data types and the
// interfaces every consumer — the HTTP API, the CLI, connectors, evals —
// builds against. The implementation is internal/engine, which this package
// never imports; tests and evals run it in-process against throwaway
// databases.
//
// One file per subject. A type lives with the thing it describes: a record in
// record.go, the verbs that write one in write.go, the shape of a read in
// query.go. There is no file here named for a language construct.
package substrate
