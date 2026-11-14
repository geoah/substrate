package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/domains"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Publishing (decision records 0289 and 0291). The owner publishes one of the
// repository's own packages under an authority whose DNS they control, and
// this server serves it from then on: the authority's document at
// /published/{repository}/{authority} and each release's closure below it,
// both public, addressed by the repository's id so a rename moves neither.
//
// THE PROOF is the authority's `_substrate` record naming that document
// (`doc=`), exactly. Only whoever controls the authority's DNS can write it,
// and it names this server and this repository, so it is the owner's
// intent and their control in one; another server that installs the package
// reads the same record to find the document (decision record 0212). The
// publish and each release check it, and refuse while it names anything
// else. A server without an HTTPS public URL publishes nothing: another
// server fetches only HTTPS.

const (
	publishRoute  = "/packages/publish"
	releaseRoute  = "/packages/release"
	releasesRoute = "/packages/releases"
	// publishedPrefix is where this server serves what its repositories
	// publish, outside /api like the webhook door.
	publishedPrefix = "/published"
)

// documentURL is the address of the document this server serves for what
// repository publishes under authority, or "" with the reason there is none.
func (h *handler) documentURL(repository, authority string) (string, string) {
	u, err := url.Parse(h.publicURL)
	if h.publicURL == "" || err != nil || u.Scheme != "https" {
		return "", "this server has no HTTPS public URL (SUBSTRATE_PUBLIC_URL), so another server cannot install what it publishes"
	}
	return h.publicURL + publishedPrefix + "/" + url.PathEscape(repository) + "/" + url.PathEscape(authority), ""
}

// proveAuthority fills plan's proof half: the record to publish at the
// authority, and whether its `_substrate` record names this repository's
// document now.
func (h *handler) proveAuthority(ctx context.Context, repository, authority string, plan *substrate.PublishPlan) {
	plan.Name = domains.Label + "." + authority
	doc, reason := h.documentURL(repository, authority)
	plan.DocumentURL = doc
	if doc == "" {
		plan.Reason = reason
		return
	}
	existing := h.publishedRecord(ctx, authority)
	want := existing
	// A release changes the document, so a pinned digest would stop every
	// read of it at the next one.
	want.Doc, want.SHA256 = doc, ""
	plan.Record = want.String()
	ctx, cancel := context.WithTimeout(ctx, domainCheckTimeout)
	defer cancel()
	rec, found, err := domains.Lookup(ctx, h.resolver, authority)
	switch {
	case err != nil:
		plan.Reason = fmt.Sprintf("the TXT record at %s cannot be read (%v): replace it with %q", plan.Name, err, plan.Record)
	case !found:
		plan.Reason = fmt.Sprintf("no TXT record opening with %q at %s: add %q, and publish once it has propagated", domains.Version, plan.Name, plan.Record)
	case rec.Doc != doc:
		plan.Reason = fmt.Sprintf("the record at %s names the document %q, not this repository's: replace it with %q", plan.Name, rec.Doc, plan.Record)
	case rec.SHA256 != "":
		plan.Reason = fmt.Sprintf("the record at %s pins the document's sha256, which every release changes: replace it with %q", plan.Name, plan.Record)
	default:
		plan.Proven = true
	}
}

// postPublish answers what publishing a package would do and, with confirm,
// does it. The answer is the publish's own transaction rolled back
// (engine PlanPublish), so a confirm runs only the publish, once, after the
// authority's proof: a preview with no blocker and a proof is a publish
// that goes through.
// withResolver hands a publish or a release the catalog's resolver, so its
// admission reads each package it requires as an installing server
// receives it.
func (h *handler) withResolver(ctx context.Context) context.Context {
	if h.catalog == nil {
		return ctx
	}
	return substrate.WithReleaseResolver(ctx, func(ctx context.Context, id string, floor int64) ([]map[string]any, error) {
		b, err := h.catalog.Resolve(ctx, id, floor)
		if err != nil {
			return nil, err
		}
		return b.Declarations(), nil
	})
}

func (h *handler) postPublish(w http.ResponseWriter, r *http.Request) {
	var in substrate.PublishInput
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	ctx := h.withResolver(r.Context())
	ds := DatasetFrom(ctx)
	actor := ActorFrom(ctx)
	if !in.Confirm {
		plan, err := ds.PlanPublish(ctx, actor, in)
		if err != nil {
			writeSubstrateError(w, r, err)
			return
		}
		authority, _ := vocabulary.SplitPackageRef(plan.Published)
		h.proveAuthority(ctx, ds.Repository().ID, authority, &plan)
		writeJSON(w, http.StatusOK, plan)
		return
	}
	// The proof is read once the publish has admitted what was asked, so a
	// refusal of the package or the name comes first, and before anything
	// is written.
	var proof substrate.PublishPlan
	ctx = substrate.WithPublishProof(ctx, func(authority string) error {
		h.proveAuthority(ctx, ds.Repository().ID, authority, &proof)
		if !proof.Proven {
			return fmt.Errorf("%w: publishing %s as %s/%s needs its authority's proof: %s", substrate.ErrForbidden, in.Package, authority, packageName(in.Package), proof.Reason)
		}
		return nil
	})
	done, err := ds.Publish(ctx, actor, in)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	done.Name, done.Record, done.DocumentURL, done.Proven = proof.Name, proof.Record, proof.DocumentURL, true
	writeJSON(w, http.StatusOK, done)
}

// postPackageRelease answers a package's next release and, with confirm,
// writes it, while the authority's record still names its document. The
// answer is the release's own transaction rolled back, and a missing proof
// is one of its blockers.
func (h *handler) postPackageRelease(w http.ResponseWriter, r *http.Request) {
	var in substrate.ReleaseInput
	if !readJSON(w, r, &in, jsonBody) {
		return
	}
	ctx := h.withResolver(r.Context())
	ds := DatasetFrom(ctx)
	var proof substrate.PublishPlan
	proved := false
	prove := func(authority string) error {
		h.proveAuthority(ctx, ds.Repository().ID, authority, &proof)
		proved = true
		if !proof.Proven {
			return fmt.Errorf("%w: releasing %s needs its authority's proof: %s", substrate.ErrForbidden, in.Package, proof.Reason)
		}
		return nil
	}
	plan, err := ds.ReleasePackage(substrate.WithPublishProof(ctx, prove), ActorFrom(ctx), in)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	// A plan names a missing proof among what refuses the release now.
	if !in.Confirm && !proved {
		authority, _ := vocabulary.SplitPackageRef(in.Package)
		if err := prove(authority); err != nil {
			plan.Blockers = append(plan.Blockers, "the release needs its authority's proof: "+proof.Reason)
		}
	}
	writeJSON(w, http.StatusOK, plan)
}

// packageName is the package word of a package reference, as the publish
// keeps it under the authority.
func packageName(ref string) string {
	_, name := vocabulary.SplitPackageRef(ref)
	return name
}

// getReleases lists the repository's releases.
func (h *handler) getReleases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	releases, err := DatasetFrom(ctx).Releases(ctx, "")
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	if releases == nil {
		releases = []substrate.Release{}
	}
	writeJSON(w, http.StatusOK, substrate.Releases{Items: releases})
}

func (h *handler) getPublishedDocument(w http.ResponseWriter, r *http.Request) {
	name := pathParam(r, "authority")
	repository := pathParam(r, "repository")
	releases, err := h.svc.PublishedReleases(r.Context(), repository, name)
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	if len(releases) == 0 {
		writeSubstrateError(w, r, fmt.Errorf("%w: nothing is published here", substrate.ErrNotFound))
		return
	}
	doc, _ := h.documentURL(repository, name)
	if doc == "" {
		writeSubstrateError(w, r, fmt.Errorf("%w: this server has no HTTPS public URL", substrate.ErrNotFound))
		return
	}
	out := authority.AuthorityDocument{Authority: name}
	for _, rel := range releases {
		_, pkg := vocabulary.SplitPackageRef(rel.Package)
		if len(out.Packages) == 0 || out.Packages[len(out.Packages)-1].Package != pkg {
			out.Packages = append(out.Packages, authority.AuthorityPackage{Package: pkg})
		}
		p := &out.Packages[len(out.Packages)-1]
		p.Versions = append(p.Versions, authority.AuthorityPackageVersion{
			Version: int(rel.Version), SHA256: rel.SHA256, Dialect: int(rel.Dialect),
			Closure: doc + "/" + url.PathEscape(pkg) + "/" + strconv.FormatInt(rel.Version, 10),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// getPublishedClosure serves one release's closure, byte for byte.
func (h *handler) getPublishedClosure(w http.ResponseWriter, r *http.Request) {
	version, err := strconv.ParseInt(pathParam(r, "version"), 10, 64)
	if err != nil || version < 1 {
		writeSubstrateError(w, r, fmt.Errorf("%w: no such release", substrate.ErrNotFound))
		return
	}
	pkg := pathParam(r, "authority") + "/" + pathParam(r, "package")
	closure, err := h.svc.PublishedClosure(r.Context(), pathParam(r, "repository"), pkg, version)
	if errors.Is(err, substrate.ErrNotFound) {
		writeSubstrateError(w, r, fmt.Errorf("%w: no such release", substrate.ErrNotFound))
		return
	}
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(closure)
}
