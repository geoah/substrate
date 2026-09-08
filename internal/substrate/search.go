package substrate

// SearchMode selects the arm(s) of search.
type SearchMode string

const (
	SearchLexical  SearchMode = "lexical"
	SearchSemantic SearchMode = "semantic"
	SearchHybrid   SearchMode = "hybrid"
)

// SearchInput is the search(q, mode, types, k) query.
type SearchInput struct {
	Q     string     `json:"q"`
	Mode  SearchMode `json:"mode,omitempty"` // default hybrid
	Kinds []string   `json:"kinds,omitempty"`
	K     int        `json:"k,omitempty"` // default 20
}

// Hit is one search result with raw per-arm scores so callers can
// threshold (resolve-before-write needs the cosine, not a rank).
type Hit struct {
	Record   *Record `json:"record"`
	Lexical  float64 `json:"lexical,omitempty"`  // ts_rank, 0 when not ranked
	Semantic float64 `json:"semantic,omitempty"` // cosine similarity, 0 when absent
}

// SearchResult is one search's answer: the ranked hits, and how much of the
// semantic index is still being built. Pending is the number of (record,
// property) pairs waiting in the repository's embed queue: a restore from the
// repository directory or a re-embed the drain is still buying. It is counted
// whenever the semantic arm was asked for (semantic and hybrid), so a non-zero
// value beside a ranking says the ranking covers a partial index; a lexical
// search reports 0.
type SearchResult struct {
	Hits    []Hit `json:"hits"`
	Pending int   `json:"pending"`
}
