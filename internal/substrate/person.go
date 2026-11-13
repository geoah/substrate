package substrate

import "context"

// PersonOf is the person a request in repository acts for: the key every
// limit per person counts under, the chat turns a person runs at once and
// the requests a person sends a second among them. A repository has one
// sign-in today, so the person is the repository's own; once sign-in names
// a person, this is the one place that changes.
func PersonOf(_ context.Context, repository string) string {
	return repository
}
