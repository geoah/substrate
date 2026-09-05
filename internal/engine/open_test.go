package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
)

// The data root is where every repository's files live, so an engine with no
// root, or a relative one, has nowhere to put them. Open refuses before it
// opens a connection: the DSN here points nowhere, and the refusal must be
// the root's, not the database's.
func TestOpenRequiresADataRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const dsn = "postgres://substrate@127.0.0.1:1/substrate?connect_timeout=1&sslmode=disable"
	for name, opts := range map[string][]engine.Option{
		"no root":       nil,
		"relative root": {engine.WithDataRoot("data")},
	} {
		all := append([]engine.Option{
			engine.WithKindsDir("../../kinds/substrate.reamde.dev/core"),
			engine.WithCredentialKey(engine.TestCredentialKey),
		}, opts...)
		svc, err := engine.Open(ctx, dsn, all...)
		if err == nil {
			_ = svc.Close()
			t.Fatalf("%s: Open succeeded without a usable data root", name)
		}
		if !errors.Is(err, engine.ErrNoDataRoot) {
			t.Fatalf("%s: err = %v, want ErrNoDataRoot", name, err)
		}
		if !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") || !strings.Contains(err.Error(), "WithDataRoot") {
			t.Fatalf("%s: the refusal must name the variable and the option, got: %v", name, err)
		}
	}
}
