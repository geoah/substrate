package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/egress"

	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// Domain verification (decision 0208): a repository proves it controls its
// name's DNS after registration. POST /repository/domain answers the
// `_substrate` TXT record to publish, with a one-time proof the repository
// record keeps pending; POST /repository/domain/verify reads the record back
// and stamps `domainVerifiedAt` when it carries that proof and its `server=`
// reaches this server. The proof in DNS is the evidence: only the name's
// controller can publish it. The `server=` comparison is a consistency check
// that the record points here, not authentication of the server, since a
// proxy in front of this server can relay its server.json. Both routes refuse
// a scoped app token; a full-access token is a sign-in and acts as the owner
// (decision 0204).

const (
	domainRoute       = "/repository/domain"
	domainVerifyRoute = "/repository/domain/verify"
	// domainCheckTimeout bounds one check's DNS lookup and server.json
	// fetch together, so a slow name cannot hold the request open.
	domainCheckTimeout = 15 * time.Second
	// maxServerDoc bounds the server.json a check reads from a `server=`
	// origin anybody may name. The real document is a few kilobytes.
	maxServerDoc = 64 << 10
)

// serverInstances reads the instance id a substrate origin serves in its
// server.json. The server's is fetchServerInstance; a test substitutes one
// that answers from memory, since the confined client never dials a loopback
// address.
type serverInstances func(ctx context.Context, origin string) (string, error)

// fetchServerInstance reads `server.instance` from origin's discovery
// document through the confined client: public addresses only, no redirects.
func fetchServerInstance(ctx context.Context, origin string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/.well-known/substrate/server.json", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := egress.ConfinedClient().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("it answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxServerDoc+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxServerDoc {
		return "", fmt.Errorf("its document is larger than %d bytes", maxServerDoc)
	}
	var doc struct {
		Server struct {
			Instance string `json:"instance"`
		} `json:"server"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("its document is not JSON: %w", err)
	}
	return doc.Server.Instance, nil
}

// domainRecord is the TXT value a repository publishes to prove its name:
// the record the name already publishes with server= and proof= set, so a
// name that is also a publisher keeps its doc= and sha256= and every key
// this binary does not read. One label carries one `v=substrate1` record, so
// the owner replaces the record rather than adding a second.
func domainRecord(existing domains.Record, server, proof string) string {
	existing.Server, existing.Proof = server, proof
	return existing.String()
}

// publishedRecord reads repository's `_substrate` record for the value to
// merge into, the zero record when there is none or it cannot be read: the
// check that follows says why a record does not prove the name.
func (h *handler) publishedRecord(ctx context.Context, repository string) domains.Record {
	ctx, cancel := context.WithTimeout(ctx, domainCheckTimeout)
	defer cancel()
	rec, found, err := domains.Lookup(ctx, h.resolver, repository)
	if err != nil || !found {
		return domains.Record{}
	}
	return rec
}

// postDomain answers the record to publish, minting the proof when none is
// pending.
func (h *handler) postDomain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	state, err := ds.StartDomainVerification(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	repository := ds.Repository().Authority
	writeJSON(w, http.StatusOK, substrate.DomainProof{
		Name:       domains.Label + "." + repository,
		Record:     domainRecord(h.publishedRecord(ctx, repository), h.issuerOf(r), state.Proof),
		Proof:      state.Proof,
		VerifiedAt: state.VerifiedAt,
	})
}

// postDomainVerify runs the check. A failed check is a 200 with a reason: the
// request succeeded and its answer is "not yet".
//
// Nothing here reads the request's Host to decide: a caller names any Host it
// likes. The own-host shortcut and the `server=` origin come from the
// operator's configured public URL alone, and without one every name needs
// the record.
func (h *handler) postDomainVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	repository := ds.Repository().Authority
	proof := ""
	if !h.underPublicHost(repository) {
		state, err := ds.DomainState(ctx)
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		if reason := h.checkDomain(ctx, repository, state.Proof, h.issuerOf(r)); reason != "" {
			writeJSON(w, http.StatusOK, substrate.DomainCheck{VerifiedAt: state.VerifiedAt, Reason: reason})
			return
		}
		proof = state.Proof
	}
	at, err := ds.MarkDomainVerified(ctx, proof)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.DomainCheck{Verified: true, VerifiedAt: &at})
}

// checkDomain answers why repository's `_substrate` record does not prove the
// name for this server, or "" when it does. Each reason names what the owner
// changes, and none carries a resolver's or a dial's own error, which would
// name the deployment's network. issuer is only the origin the reasons
// suggest; the checks never trust it unless it is the configured public URL.
func (h *handler) checkDomain(ctx context.Context, repository, proof, issuer string) string {
	label := domains.Label + "." + repository
	if proof == "" {
		return "no verification is pending: start one with Verify domain, then publish the record it shows at " + label
	}
	ctx, cancel := context.WithTimeout(ctx, domainCheckTimeout)
	defer cancel()
	rec, found, err := domains.Lookup(ctx, h.resolver, repository)
	want := domainRecord(rec, issuer, proof)
	switch {
	case errors.Is(err, domains.ErrMalformed):
		return fmt.Sprintf("the TXT record at %s cannot be read (%v): replace it with %q", label, err, domainRecord(domains.Record{}, issuer, proof))
	case err != nil:
		return fmt.Sprintf("the DNS lookup of %s did not answer: try again once the name's DNS answers", label)
	case !found:
		return fmt.Sprintf("no TXT record opening with %q at %s: add %q, and check again once it has propagated", domains.Version, label, want)
	case rec.Proof == "":
		return fmt.Sprintf("the record at %s carries no proof=: replace it with %q", label, want)
	case rec.Proof != proof:
		return fmt.Sprintf("the record at %s carries proof=%s, and the pending proof is %s: replace it with %q", label, rec.Proof, proof, want)
	case rec.Server == "":
		return fmt.Sprintf("the record at %s names no server=: replace it with %q", label, want)
	}
	// With a configured public URL, server= must name exactly that origin,
	// and that decides it: the proof shows the name's controller pointed the
	// record here, and reading the origin's server.json back would add
	// nothing, while a server whose public name resolves to a private address
	// from inside could never read it.
	if h.publicURL != "" {
		if rec.Server != strings.ToLower(h.publicURL) {
			return fmt.Sprintf("server=%s names another substrate: point it at %s", rec.Server, h.publicURL)
		}
		return ""
	}
	// Without one, the instance comparison is a consistency check that
	// server= reaches this server, not authentication of the server: a proxy
	// that forwards to this server answers with this server's id.
	instance, err := h.instances(ctx, rec.Server)
	if err != nil {
		return fmt.Sprintf("server=%s could not be read as a substrate: point server= at %s", rec.Server, issuer)
	}
	if mine := h.svc.InstanceID(); mine == "" || instance != mine {
		return fmt.Sprintf("server=%s names another substrate: point it at %s", rec.Server, issuer)
	}
	return ""
}

// underPublicHost reports whether repository is exactly one label under the
// host of the configured public URL: a name the operator controls the DNS
// of, so it is verified without a record. With no public URL there is no
// such host, and every name needs the record.
func (h *handler) underPublicHost(repository string) bool {
	if h.publicURL == "" {
		return false
	}
	u, err := url.Parse(h.publicURL)
	if err != nil {
		return false
	}
	return underHost(repository, u.Hostname())
}

// underHost reports whether repository is `<label>.<host>` for one label.
func underHost(repository, host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	label, ok := strings.CutSuffix(repository, "."+host)
	return ok && label != "" && !strings.Contains(label, ".")
}
