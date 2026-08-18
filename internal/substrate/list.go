package substrate

// OperationalList is the envelope every operational list answers with: the
// unbounded sets that are not the record stream — tokens, the catalog, trigger
// and bundle status, parked deliveries, trait implementors. Items always holds
// the whole set today; Cursor is reserved so keyset pagination lands as a
// filled field, not a reshaped body (decision 0036). Records, history and
// incoming keep their own keyset Page (records/cursor/head, query.go).
type OperationalList[T any] struct {
	Items  []T    `json:"items"`
	Cursor string `json:"cursor,omitempty"`
}

// Listed wraps items in the operational-list envelope, normalizing a nil slice
// to an empty one so the wire carries [] rather than null.
func Listed[T any](items []T) OperationalList[T] {
	if items == nil {
		items = []T{}
	}
	return OperationalList[T]{Items: items}
}
