package substrate

import "context"

type tailReadCtx struct{}

// WithTailRead marks ctx as a stream following the changelog's tail: its
// Changes reads ask, again and again, for what was committed after its
// cursor, so the implementation may answer every such stream from one shared
// read of the tail instead of one read each. A one-off read stays unmarked.
func WithTailRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, tailReadCtx{}, true)
}

// TailRead reports whether ctx follows the tail (WithTailRead).
func TailRead(ctx context.Context) bool {
	v, _ := ctx.Value(tailReadCtx{}).(bool)
	return v
}
