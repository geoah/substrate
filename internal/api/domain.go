package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
)

// Domain verification (decision 0208): a repository proves it controls its
// name's DNS after registration. POST /repository/domain answers the
// `_substrate` TXT record to publish, with a one-time proof the repository
// record keeps pending; POST /repository/domain/verify reads the record back
// and stamps `domainVerifiedAt` when it carries that proof and its `server=`
// origin is this server. Both are the owner's: an app's token is refused.

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
	resp, err := authorize.ConfinedClient().Do(req)
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

// domainRecord is the TXT value a repository publishes to prove its name.
func domainRecord(server, proof string) string {
	return domains.Version + " server=" + server + " proof=" + proof
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
	writeJSON(w, http.StatusOK, substrate.DomainProof{
		Name:       domains.Label + "." + ds.Repository().ID,
		Record:     domainRecord(h.issuerOf(r), state.Proof),
		Proof:      state.Proof,
		VerifiedAt: state.VerifiedAt,
	})
}

// postDomainVerify runs the check. A failed check is a 200 with a reason: the
// request succeeded and its answer is "not yet".
func (h *handler) postDomainVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ds := DatasetFrom(ctx)
	repository := ds.Repository().ID
	if !underOwnHost(repository, r.Host) {
		state, err := ds.DomainState(ctx)
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		if reason := h.checkDomain(ctx, repository, state.Proof, h.issuerOf(r)); reason != "" {
			writeJSON(w, http.StatusOK, substrate.DomainCheck{VerifiedAt: state.VerifiedAt, Reason: reason})
			return
		}
	}
	at, err := ds.MarkDomainVerified(ctx)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, substrate.DomainCheck{Verified: true, VerifiedAt: &at})
}

// checkDomain answers why repository's `_substrate` record does not prove the
// name for this server, or "" when it does. Each reason names what the owner
// changes.
func (h *handler) checkDomain(ctx context.Context, repository, proof, issuer string) string {
	label := domains.Label + "." + repository
	if proof == "" {
		return "no verification is pending: start one with Verify domain, then publish the record it shows at " + label
	}
	want := domainRecord(issuer, proof)
	ctx, cancel := context.WithTimeout(ctx, domainCheckTimeout)
	defer cancel()
	rec, found, err := domains.Lookup(ctx, h.resolver, repository)
	switch {
	case errors.Is(err, domains.ErrMalformed):
		return fmt.Sprintf("the TXT record at %s cannot be read (%v): replace it with %q", label, err, want)
	case err != nil:
		return fmt.Sprintf("the DNS lookup of %s failed (%v): try again once the name's DNS answers", label, err)
	case !found:
		return fmt.Sprintf("no TXT record opening with %q at %s: add %q, and check again once it has propagated", domains.Version, label, want)
	case rec.Proof == "":
		return fmt.Sprintf("the record at %s carries no proof=: add proof=%s", label, proof)
	case rec.Proof != proof:
		return fmt.Sprintf("the record at %s carries proof=%s, and the pending proof is %s: replace it with %q", label, rec.Proof, proof, want)
	case rec.Server == "":
		return fmt.Sprintf("the record at %s names no server=: add server=%s", label, issuer)
	}
	instance, err := h.instances(ctx, rec.Server)
	if err != nil {
		return fmt.Sprintf("server=%s could not be read as a substrate (%v): point server= at %s", rec.Server, err, issuer)
	}
	if mine := h.svc.InstanceID(); mine == "" || instance != mine {
		return fmt.Sprintf("server=%s names another substrate: point it at %s", rec.Server, issuer)
	}
	return ""
}

// underOwnHost reports whether repository is `<label>.<host>` for the host
// the request reached: a name the server's operator controls the DNS of, so
// it is verified without a record. host is the Host header, port included.
func underOwnHost(repository, host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return false
	}
	label, ok := strings.CutSuffix(repository, "."+host)
	return ok && label != "" && !strings.Contains(label, ".")
}
