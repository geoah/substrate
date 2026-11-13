package substrate

import "context"

// The IDENTITY of a request is the person its token acts for (decision 0274),
// read off the token record by the door that authenticated it. It rides the
// context beside the principal, for the same reason: the only writer is
// `withRequestAuth` (internal/api), and a caller cannot name it.
type identityKey struct{}

// WithIdentity binds the identity a request's token acts for. An empty id
// leaves ctx unmarked.
func WithIdentity(ctx context.Context, identity string) context.Context {
	if identity == "" {
		return ctx
	}
	return context.WithValue(ctx, identityKey{}, identity)
}

// IdentityFrom returns the identity a request's token acts for, empty when
// none does: a token no person stands behind, or work no request started.
func IdentityFrom(ctx context.Context) string {
	id, _ := ctx.Value(identityKey{}).(string)
	return id
}

// PersonOf is the person a request in repository acts for: the key every
// limit per person counts under, the chat turns a person runs at once and
// the requests a person sends a second among them. It is the identity the
// request's token acts for, so one person's requests count together in
// every repository they open. Work no person stands behind counts under the
// repository it runs in.
func PersonOf(ctx context.Context, repository string) string {
	if id := IdentityFrom(ctx); id != "" {
		return id
	}
	return repository
}
