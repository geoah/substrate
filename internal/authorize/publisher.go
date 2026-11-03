package authorize

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/domains"
)

type (
	AuthorityDocument       = authority.AuthorityDocument
	AuthorityPackage        = authority.AuthorityPackage
	AuthorityPackageVersion = authority.AuthorityPackageVersion
	AuthorityApp            = authority.AuthorityApp
	AppleApps               = authority.AppleApps
)

var (
	ParseAuthorityDocument = authority.ParseAuthorityDocument
	CheckLocalDocuments    = authority.CheckLocalDocuments
)

type PublisherOptions struct {
	Resolver       domains.Resolver
	Client         *http.Client
	Now            func() time.Time
	LocalDocuments map[string]string
	Reader         *authority.Reader
}
type Verifier struct{ reader *authority.Reader }

func NewVerifier(o PublisherOptions) *Verifier {
	r := o.Reader
	if r == nil {
		r = authority.NewReader(authority.Options{Resolver: o.Resolver, Client: o.Client, Now: o.Now, LocalDocuments: o.LocalDocuments})
	}
	return &Verifier{reader: r}
}

// Publisher is what the server could confirm about who publishes an app.
// Verified is true only when every step held; otherwise Reason says, for
// the owner, which step did not, and is empty for an app that claims no
// publisher at all.
type Publisher struct {
	Verified bool
	// Authority is the verified authority, or the one the app claims.
	Authority string
	// Name is the authority document's own name for the publisher, and
	// AppName its name for this app; both only when the document was read.
	Name    string
	AppName string
	Reason  string
	// Record is the `_substrate` record as the server read it, DocURL the
	// document it names and Listed whether that document lists the client
	// id: the technical details behind the verdict.
	Record string
	DocURL string
	Listed bool
	// FromFile is true when the document came from a file this server was
	// configured to read (SUBSTRATE_INSECURE_AUTHORITY_DOCS) in place of the
	// authority's DNS record and the fetch: the operator, not the
	// authority's DNS, vouched for it.
	FromFile bool
}

// Same reports whether p and q are the same answer to who publishes an app:
// both unverified, or both verified as the same authority by the same
// source. An approval and a token exchange hold the publisher a later check
// finds to the one the owner was shown.
func (p Publisher) Same(q Publisher) bool {
	if p.Verified != q.Verified {
		return false
	}
	return !p.Verified || (p.Authority == q.Authority && p.FromFile == q.FromFile)
}

// Verify decides who publishes the app c, whose client document is doc, for
// a request whose code goes to redirect. A loopback client id or a loopback
// redirect is never verified: any program on the device can send either.
func (v *Verifier) Verify(ctx context.Context, c Client, doc Document, redirect string) Publisher {
	if c.Local {
		return Publisher{Reason: "the app runs on this device, and nothing confirms which program it is"}
	}
	p := Publisher{Authority: doc.Authority}
	if doc.RefusedAuthority != "" {
		p.Reason = fmt.Sprintf("its document claims the publisher %q, which is not an authority name", doc.RefusedAuthority)
		return p
	}
	if doc.Authority == "" {
		return p
	}
	authority := doc.Authority
	source, err := v.reader.Read(ctx, authority, false)
	p.Record, p.DocURL, p.FromFile = source.Record, source.DocURL, source.FromFile
	adoc := source.Document
	if err != nil {
		p.Reason = err.Error()
		return p
	}
	p.Name = adoc.Name
	entry, listed := adoc.App(c.ID)
	p.Listed = listed
	if !listed {
		p.Reason = fmt.Sprintf("the authority document of %s does not list this app's client id", authority)
		return p
	}
	p.AppName = entry.Name
	if LocalRedirect(redirect) {
		p.Reason = "the access goes to a program on this device, and nothing confirms which program it is"
		return p
	}
	if !RedirectUnder(redirect, authority) {
		p.Reason = fmt.Sprintf("the access goes to %s, which is not %s or a name under it", HostOf(redirect), authority)
		return p
	}
	p.Verified = true
	return p
}

// RedirectUnder reports whether redirect is https on the host authority or a
// name under it.
func RedirectUnder(redirect, authority string) bool {
	u, err := ParseRedirect(redirect)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == authority || strings.HasSuffix(host, "."+authority)
}
