package substrate

import "context"

// The IDEMPOTENCY KEY is the `Idempotency-Key` request header, the client's
// name for one attempt at an operation whose effect the server assigns: a
// create without an id, a function or agent call, a merge, a split. It rides
// the context the way the principal does, because every one of those entry
// points already takes one and none of their inputs is the right place for a
// value that is about the REQUEST rather than the record. The HTTP layer is
// the only writer, and only on the handlers the docs list (docs/api.md,
// "Idempotency and retries"); a stream (agent chat) never carries one.
//
// An implementation that honors the key returns the stored outcome of the
// first attempt when the same key arrives again with the same input, refuses
// the same key with a different input as ErrConflict, and runs the effect
// once. A key binds to the repository and the operation, never to the token
// that carried it.
type idempotencyKeyCtx struct{}

// MaxIdempotencyKeyLength bounds the header value. A key is an opaque client
// string, and a bound keeps the key table's row size independent of what a
// client decides to send.
const MaxIdempotencyKeyLength = 255

// WithIdempotencyKey binds a request's idempotency key to ctx. An empty key
// binds nothing.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKeyCtx{}, key)
}

// WithoutIdempotencyKey returns ctx with no idempotency key. An entry point
// that consumed the key runs its body on this context, so a nested write (an
// agent's mutate tool, a function's effects) cannot mistake the request's key
// for its own.
func WithoutIdempotencyKey(ctx context.Context) context.Context {
	if IdempotencyKeyFrom(ctx) == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKeyCtx{}, "")
}

// IdempotencyKeyFrom returns the request's idempotency key, empty when the
// request carried none.
func IdempotencyKeyFrom(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyCtx{}).(string)
	return key
}
