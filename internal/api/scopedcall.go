package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// CallFunction runs a function the grant's `call:` scopes name (decision
// 0219), and only one whose own reach the grant's read and write scopes
// cover: a call runs the owner's installed code, so what the code may read
// and write is held to what the owner let the app read and write. A function
// the grant does not call is answered as one the repository never declared,
// so a refusal does not tell an app which functions exist. The function's
// effects run under the function's own permissions, as a trigger's do, so
// the scoped mark stops here (decision 0195 holds the same for a chat).
func (s *scopedDataset) CallFunction(ctx context.Context, actor substrate.Actor, name string, args any) (any, int, error) {
	if !s.grant.CallsAny() {
		return nil, 0, errScope("functions")
	}
	if authority, _, _ := vocabulary.SplitKindRef(name); authority == "" {
		return nil, 0, fmt.Errorf("%w: an app's token names a function in full, <authority>/<package>/<name>, not %q",
			substrate.ErrValidation, name)
	}
	name = s.local(name)
	if !s.grant.Calls(name) {
		return nil, 0, fmt.Errorf("%w: unknown function %q", substrate.ErrNotFound, name)
	}
	fn, err := s.declarations().Get(ctx, kindFunction, name)
	if err != nil {
		return nil, 0, err
	}
	reach, err := resolveReach(ctx, s.declarations(), fn)
	if err != nil {
		return nil, 0, err
	}
	reg, err := s.kinds(ctx)
	if err != nil {
		return nil, 0, err
	}
	if uncovered := uncoveredReach(s.grant, reach, reg); len(uncovered) > 0 {
		return nil, 0, fmt.Errorf("%w: an app's token calls %s only where its scopes cover what the function reaches, and they do not cover: %s",
			substrate.ErrForbidden, name, strings.Join(uncovered, ", "))
	}
	return s.inner.CallFunction(substrate.WithoutScopes(ctx), s.writer(actor), name, args)
}
