// Package gql is the repository's GraphQL surface as a LIBRARY: the schema
// generated from the kind registry, the resolvers against the substrate
// contract, and a fingerprint-keyed schema cache. Two callers execute against
// it and neither owns it: the API's /graphql endpoint (internal/api) and the
// agent loop's graphql/mutate built-in tools (internal/engine). It depends
// only on internal/substrate and internal/vocabulary, so the house rule holds:
// the API never imports the engine, and the engine never imports the API.
package gql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"sync"

	"github.com/graphql-go/graphql"

	"github.com/geoah/substrate/internal/substrate"
)

type ctxKey int

const (
	ctxKeyDataset ctxKey = iota
	ctxKeyActor
)

// WithRequest binds the dataset and actor one execution resolves against.
// Each caller sets it explicitly right before graphql.Do: the resolvers read
// ONLY this context, never a transport's.
func WithRequest(ctx context.Context, ds substrate.Dataset, actor substrate.Actor) context.Context {
	ctx = context.WithValue(ctx, ctxKeyDataset, ds)
	return context.WithValue(ctx, ctxKeyActor, actor)
}

// DatasetFrom returns the bound dataset, or nil outside an execution.
func DatasetFrom(ctx context.Context) substrate.Dataset {
	ds, _ := ctx.Value(ctxKeyDataset).(substrate.Dataset)
	return ds
}

// ActorFrom returns the bound actor; empty outside an execution.
func ActorFrom(ctx context.Context) substrate.Actor {
	actor, _ := ctx.Value(ctxKeyActor).(substrate.Actor)
	return actor
}

// cachedSchema is one repository's built schema plus the registry fingerprint
// it was built from.
type cachedSchema struct {
	key    string
	schema graphql.Schema
}

// Cache holds one built schema per repository, rebuilt when the registry
// fingerprint moves. Each surface holds its own (the API handler, the engine
// service); the entries are identical because the key and builder are.
type Cache struct {
	mu           sync.Mutex
	byRepository map[string]*cachedSchema
}

func NewCache() *Cache {
	return &Cache{byRepository: map[string]*cachedSchema{}}
}

// SchemaFor returns the repository's schema, rebuilding it when the type
// registry's fingerprint changed (connector installs, schema deploys).
func (c *Cache) SchemaFor(repository string, types []substrate.KindInfo) (*graphql.Schema, error) {
	key := RegistryKey(types)
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.byRepository[repository]; ok && cached.key == key {
		return &cached.schema, nil
	}
	schema, err := BuildSchema(types)
	if err != nil {
		return nil, err
	}
	cached := &cachedSchema{key: key, schema: schema}
	c.byRepository[repository] = cached
	return &cached.schema, nil
}

// RegistryKey fingerprints a kind registry for the schema cache.
func RegistryKey(types []substrate.KindInfo) string {
	ids := make([]string, 0, len(types))
	for _, t := range types {
		// The schema builds fields from the DEFINITION, so the key must move
		// with it: schema is records, and a property added through the
		// record path activates on commit — not on the next type add/remove.
		// json.Marshal sorts map keys, so equal definitions hash equal.
		def, _ := json.Marshal(t.Definition)
		ids = append(ids, t.Identity+"@"+strconv.FormatInt(t.Version, 10)+"@"+string(def))
	}
	sort.Strings(ids)
	sum := sha256.New()
	for _, id := range ids {
		_, _ = sum.Write([]byte(id))
		_, _ = sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
