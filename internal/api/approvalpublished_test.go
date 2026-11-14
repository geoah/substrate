package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

// Decision 0214: a package detail resolves through the binary's catalog and
// the authority documents; `declare:` and the `agent` detail are refused by
// name; a build says its version and the token and the check record it; and
// an app reaches an agent only when its own reads cover the agent's. The
// refusals at the door are the handler's, here; what an approval or a check
// writes and what a token reaches is the engine's, in
// approvalpublished_db_test.go.

const (
	dpAuthority = "example.org"
	dpTraining  = dpAuthority + "/training"
	dpSession   = dpTraining + "/session"
	dpDocURL    = "https://example.org/.well-known/substrate/authority.json"
)

// publishingCatalog is the real catalog with one package an authority
// document publishes beside it, answering Resolve as the catalog's document
// reader will: the binary wins for every id it ships, the listed package
// resolves at the floor or below its version, and anything else is not
// published with the reason.
type publishingCatalog struct {
	*catalog.Catalog
	training *catalog.Bundle
}

// trainingClosure is the training package's closure as example.org lists it
// at version.
func trainingClosure(version int64) string {
	return "kind: substrate.reamde.dev/core/package\nmetadata:\n  id: " + dpTraining + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  version: " + strconv.FormatInt(version, 10) + "\n" +
		"---\nkind: substrate.reamde.dev/core/bundle\nmetadata:\n  id: " + dpTraining + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  description: Strength sessions.\n  installs: [" + dpSession + "]\n" +
		"---\nkind: substrate.reamde.dev/core/kind\nmetadata:\n  id: " + dpSession + "\ndata:\n  authority: " + dpAuthority + "\n  package: training\n  description: A strength session.\n  names:\n    singular: session\n  label:\n    singular: Session\n    plural: Sessions\n"
}

func newPublishingCatalog(t *testing.T, cat *catalog.Catalog) *publishingCatalog {
	t.Helper()
	closure := trainingClosure(3)
	docs, err := catalog.Load(catalog.ProviderRoot(fstest.MapFS{"example.org/training/bundle.yaml": {Data: []byte(closure)}}))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := docs.ByID(dpTraining)
	if !ok || len(docs.Warnings()) > 0 {
		t.Fatalf("the published fixture did not load: %v", docs.Warnings())
	}
	return &publishingCatalog{Catalog: cat, training: b.FromDocument(catalog.Origin{
		Authority: dpAuthority, Name: "Example Publishing", DocumentURL: dpDocURL,
		ClosureURL: "https://example.org/packages/training-3.yaml", SHA256: strings.Repeat("cd", 32),
	})}
}

// ResolveFor answers as Resolve: the own-authority refusal is the
// catalog's, and its own tests hold it.
func (c *publishingCatalog) ResolveFor(ctx context.Context, id string, atLeast int64, _ substrate.Dataset) (*catalog.Bundle, error) {
	return c.Resolve(ctx, id, atLeast)
}

func (c *publishingCatalog) Resolve(_ context.Context, id string, atLeast int64) (*catalog.Bundle, error) {
	if b, ok := c.Catalog.ByID(id); ok {
		return b, nil
	}
	if id != dpTraining {
		return nil, fmt.Errorf("%w: %s publishes no document listing %s", catalog.ErrNotPublished, dpAuthority, id)
	}
	if c.training.Version < atLeast {
		return nil, fmt.Errorf("%w: %s lists %s at version %d, and the app needs %d or later", catalog.ErrNotPublished, dpAuthority, id, c.training.Version, atLeast)
	}
	return c.training, nil
}

func (c *publishingCatalog) Upgrade(ctx context.Context, id string, ds substrate.Dataset, held *substrate.BundleStatus) (*substrate.BundleUpgrade, error) {
	if id == dpTraining {
		return nil, nil
	}
	return c.Catalog.Upgrade(ctx, id, ds, held)
}

// refreshes records what the handler asked the catalog to refresh.
type refreshes struct {
	mu    sync.Mutex
	asked []string
	err   error
}

func (r *refreshes) RefreshPackage(_ context.Context, ds substrate.Dataset, id string) (catalog.RefreshResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, ds.Repository().Authority+" "+id)
	return catalog.RefreshResult{ID: id, Outcome: "current"}, r.err
}

func (r *refreshes) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

// THE RETIRED GRAMMAR IS REFUSED BY NAME: a `declare:` scope and an `agent`
// detail are refused at the authorization endpoint and at the check, each
// pointing at the package detail that replaced them, and the metadata lists
// neither.
//
// Each refusal comes before the check records the build: the stub records
// none, so reaching it would answer 500.
func TestADeclareScopeAndAnAgentDetailAreRefusedAtTheDoor(t *testing.T) {
	e := newAuthorizeStub(t)
	md := decodeJSON[authorizationServerMetadata](t, e.do(http.MethodGet, asMetadataPath, "", nil, ""))
	if strings.Join(md.ScopesSupported, " ") != "read:* write:* agents:* decide:* call:* full mcp" {
		t.Fatalf("scopes_supported = %v", md.ScopesSupported)
	}
	agentDetail := `[{"type":"agent","agent":"~/trainingcoach/workoutCoach","reads":["~/training/routine"],"writes":[],"spendCentsPerDay":100}]`
	for name, params := range map[string]map[string]string{
		"a declare scope": appRequest("declare:~/training write:~/training/*"),
		"an agent detail": setupRequest("write:~/training/*", agentDetail),
		"a version too long": func() map[string]string {
			p := appRequest("read:~/notes/*")
			p["software_version"] = strings.Repeat("9", 65)
			return p
		}(),
	} {
		rec := e.do(http.MethodGet, authorizeURL(params), "", nil, "")
		retired := name != "a version too long"
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "retired") != retired {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	for name, body := range map[string]string{
		"a declare scope":    `{"scope":"declare:~/training","details":[]}`,
		"an agent detail":    `{"scope":"read:~/notes/*","details":` + agentDetail + `}`,
		"a version too long": `{"scope":"read:~/notes/*","details":[],"softwareVersion":"` + strings.Repeat("9", 65) + `"}`,
	} {
		if rec := e.do(http.MethodPost, "/api/v1/authorizations/check", e.owner, []byte(body), "application/json"); rec.Code != http.StatusBadRequest {
			t.Errorf("the check with %s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}
