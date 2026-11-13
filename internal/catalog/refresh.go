package catalog

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/authority"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// RefreshResult records one publisher check, including a lapsed publisher.
type RefreshResult struct {
	ID      string
	Outcome string
	Reason  string
}

// Refresh checks every document-published package, one at a time.
func (c *Catalog) Refresh(ctx context.Context, ds substrate.Dataset) ([]RefreshResult, error) {
	statuses, err := ds.BundleStatusesUncounted(ctx)
	if err != nil {
		return nil, err
	}
	var out []RefreshResult
	for _, st := range statuses {
		if !st.Installed {
			continue
		}
		row, err := ds.Get(ctx, vocabulary.PackageCore+"/package", st.ID)
		if err != nil {
			return out, err
		}
		if mstr(row.Properties, "publishedFrom") == "" {
			continue
		}
		result, err := c.RefreshPackage(ctx, ds, st.ID)
		if err != nil {
			result = RefreshResult{ID: st.ID, Outcome: "upgrade-needs-review", Reason: err.Error()}
		}
		out = append(out, result)
	}
	return out, nil
}

// RefreshPackage takes the newest unheld additive release, leaving other
// changes for the owner's existing upgrade door.
func (c *Catalog) RefreshPackage(ctx context.Context, ds substrate.Dataset, id string) (RefreshResult, error) {
	result := RefreshResult{ID: id, Outcome: "current"}
	row, err := ds.Get(ctx, vocabulary.PackageCore+"/package", id)
	if errors.Is(err, substrate.ErrNotFound) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if mstr(row.Properties, "publishedFrom") == "" {
		return result, nil
	}
	// The binary wins for every package it ships (decision 0212), on a
	// refresh as on an install: a package a later binary ships is never
	// upgraded from a document again.
	if c.ships(id) {
		result.Outcome = "upgrade-needs-review"
		result.Reason = id + " now ships with this server; its catalog offers the shipped version"
		return result, nil
	}
	source, err := c.reader.Read(ctx, mstr(row.Properties, "authority"), true)
	if err != nil {
		return result, err
	}
	// The record now names another document than the package came from: a
	// publisher that moved its files, or a DNS answer that should not be
	// followed unseen. Nothing upgrades by itself until the owner looks.
	if from := mstr(row.Properties, "publishedFrom"); source.DocURL != from {
		result.Outcome = "upgrade-needs-review"
		result.Reason = fmt.Sprintf("the publisher's document moved from %s to %s", from, source.DocURL)
		return result, nil
	}
	held := mversion(row.Properties, "shippedVersion")
	versions := listedVersions(source, id)
	for _, v := range versions {
		if int64(v.Version) <= held {
			break
		}
		// A version written for a newer dialect is not this server's to
		// install; the newest it can read is the upgrade (decision 0268).
		if !readableVersion(v) {
			continue
		}
		b, err := c.readClosure(ctx, id, source, v)
		if errors.Is(err, ErrNewerDialect) {
			continue
		}
		if err != nil {
			return result, err
		}
		plan, err := c.publishedUpgrade(ctx, b, ds)
		if err != nil {
			return result, err
		}
		if len(plan.Holders) > 0 {
			result.Outcome = "upgrade-needs-review"
			result.Reason = "connected app builds hold back this release"
			continue
		}
		if plan.ReviewRequired || len(plan.Blockers) > 0 {
			result.Outcome = "upgrade-needs-review"
			result.Reason = "the publisher's changes need your review"
			return result, nil
		}
		if _, err := c.installed(ctx, b, ds, nil); err != nil {
			return result, err
		}
		result.Outcome = "upgraded"
		return result, nil
	}
	return result, nil
}

func listedVersions(source authority.Source, id string) []authority.AuthorityPackageVersion {
	_, name := vocabulary.SplitPackageRef(id)
	for _, p := range source.Document.Packages {
		if p.Package == name {
			vs := append([]authority.AuthorityPackageVersion(nil), p.Versions...)
			sort.Slice(vs, func(i, j int) bool { return vs[i].Version > vs[j].Version })
			return vs
		}
	}
	return nil
}

func (c *Catalog) heldClosure(ctx context.Context, b *Bundle, ds substrate.Dataset) (*Bundle, error) {
	row, err := ds.Get(ctx, vocabulary.PackageCore+"/package", b.ID)
	if err != nil {
		return nil, err
	}
	if mstr(row.Properties, "publishedFrom") == "" {
		return nil, fmt.Errorf("%s is not following its publisher", b.ID)
	}
	source, err := c.reader.Read(ctx, b.Authority, false)
	if err != nil {
		return nil, err
	}
	held := mversion(row.Properties, "shippedVersion")
	for _, v := range listedVersions(source, b.ID) {
		if int64(v.Version) != held {
			continue
		}
		if !strings.EqualFold(v.SHA256, mstr(row.Properties, "publishedDigest")) {
			return nil, fmt.Errorf("the publisher changed the digest of held version %d", held)
		}
		return c.readClosure(ctx, b.ID, source, v)
	}
	return nil, fmt.Errorf("the publisher no longer lists held version %d", held)
}

func (c *Catalog) publishedUpgrade(ctx context.Context, b *Bundle, ds substrate.Dataset) (*substrate.BundleUpgrade, error) {
	plan, err := c.planUpgrade(ctx, b, ds)
	if err != nil {
		return nil, err
	}
	plan.CandidateVersion = b.Version
	plan.CandidateDigest = b.origin.SHA256
	// A rename is refused whatever the repository holds; the preview names
	// it once, where the engine's own dry run did not.
	for _, refusal := range b.RenameRefusals() {
		if !slices.Contains(plan.Blockers, refusal) {
			plan.Blockers = append(plan.Blockers, refusal)
		}
	}
	old, err := c.heldClosure(ctx, b, ds)
	if err != nil {
		plan.Blockers = append(plan.Blockers, err.Error())
		plan.ReviewRequired = true
		return plan, nil
	}
	narrows, additive := CompareClosures(old, b)
	// Code whose reach grows past the held closure's waits for the owner
	// (decision 0215), whatever else the comparison concludes.
	plan.ReviewRequired = !additive || len(ReachGrows(old.Reach(), b.Reach())) > 0
	// A kind the held version published is not the publisher's to remove:
	// no review takes such a release (installed refuses it).
	for _, kind := range RemovedKinds(old, b) {
		plan.Blockers = append(plan.Blockers, removedKindRefusal(b, kind))
		plan.ReviewRequired = true
	}
	if narrows {
		tokens, err := ds.Tokens(ctx)
		if err != nil {
			return nil, err
		}
		plan.Holders = BuildHolders(tokens, b.ID, b.Version, time.Now())
		for _, holder := range plan.Holders {
			checked := "no check-in recorded"
			if holder.CheckedAt != nil {
				checked = "last checked " + holder.CheckedAt.Format(time.RFC3339)
			}
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s (%s), build %q, asks for version %d; %s", holder.ClientName, holder.ClientID, holder.SoftwareVersion, holder.Floor, checked))
		}
	}
	return plan, nil
}

// removedKindRefusal says why a release that drops a published kind is not
// taken.
func removedKindRefusal(b *Bundle, kind string) string {
	return fmt.Sprintf("version %d removes %s, a kind the publisher published; a published kind stays declared (deprecated: true), so this release is not taken", b.Version, kind)
}

// BuildHolders names only live builds that explicitly recorded a lower floor.
func BuildHolders(tokens []substrate.TokenInfo, id string, candidate int64, now time.Time) []substrate.BundleUpgradeHolder {
	var out []substrate.BundleUpgradeHolder
	for _, t := range tokens {
		floor, ok := t.AppPackages[id]
		if !ok || floor >= candidate || (t.ExpiresAt != nil && !t.ExpiresAt.After(now)) {
			continue
		}
		out = append(out, substrate.BundleUpgradeHolder{ClientName: t.ClientName, ClientID: t.ClientID, SoftwareVersion: t.AppSoftwareVersion, CheckedAt: t.AppCheckedAt, Floor: floor})
	}
	return out
}

// CompareClosures distinguishes removal from additive vocabulary. Unknown
// changes conservatively need review; execution declarations must be identical.
func CompareClosures(old, next *Bundle) (narrows, additive bool) {
	additive = true
	before := closureMembers(old)
	after := closureMembers(next)
	for key, doc := range before {
		candidate, ok := after[key]
		if !ok {
			narrows = true
			additive = false
			continue
		}
		if removes(doc, candidate) {
			narrows = true
		}
		if reflect.DeepEqual(doc, candidate) {
			continue
		}
		kind := mstr(doc, "kind")
		if kind != vocabulary.PackageCore+"/kind" && kind != vocabulary.PackageCore+"/package" && kind != vocabulary.PackageCore+"/bundle" {
			additive = false
			continue
		}
		if !safeAddition(doc, candidate) {
			additive = false
		}
	}
	for key, doc := range after {
		if _, ok := before[key]; !ok && mstr(doc, "kind") != vocabulary.PackageCore+"/kind" {
			additive = false
		}
		// A new kind that names another in movedFrom is a rename, which a
		// published package may not make (decision 0269): never additive,
		// whether or not a repository holds the kind it names.
		if _, ok := before[key]; !ok && mstr(mmap(doc, "data"), "movedFrom") != "" {
			additive = false
		}
	}
	return narrows, additive && !narrows
}

func closureMembers(b *Bundle) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, d := range append(append([]map[string]any{}, b.vocabularyDocs...), b.dataDocs...) {
		copyDoc := map[string]any{}
		for k, v := range d {
			copyDoc[k] = v
		}
		data := map[string]any{}
		for k, v := range mmap(d, "data") {
			if k != "version" {
				data[k] = v
			}
		}
		copyDoc["data"] = data
		out[mstr(d, "kind")+"\x00"+docID(d)] = copyDoc
	}
	return out
}

// removes detects vanished names and values recursively, including enum entries
// and machine states. Scalar edits need review but are not removal by themselves.
func removes(old, next any) bool {
	switch a := old.(type) {
	case map[string]any:
		b, ok := next.(map[string]any)
		if !ok {
			return true
		}
		for k, v := range a {
			n, exists := b[k]
			if !exists {
				return true
			}
			if removes(v, n) {
				return true
			}
		}
	case []any:
		b, ok := next.([]any)
		if !ok {
			return true
		}
		for _, v := range a {
			found := false
			for _, n := range b {
				if reflect.DeepEqual(v, n) {
					found = true
					break
				}
			}
			if !found {
				return true
			}
		}
	}
	return false
}

func safeAddition(old, next any) bool {
	switch a := old.(type) {
	case map[string]any:
		b, ok := next.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range a {
			if !safeAddition(v, b[k]) {
				return false
			}
		}
		for k, v := range b {
			if _, ok := a[k]; ok {
				continue
			}
			// New execution blocks, constraints, renames and migrations need consent.
			switch k {
			case "oauth2", "inputs", "modules", "module", "required", "renamedFrom", "movedFrom", "validation", "validations", "min", "max", "pattern", "default":
				return false
			}
			if hasRequired(v) {
				return false
			}
		}
		return true
	case []any:
		b, ok := next.([]any)
		if !ok {
			return false
		}
		for _, v := range a {
			found := false
			for _, n := range b {
				if reflect.DeepEqual(v, n) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(old, next)
	}
}

func hasRequired(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if x["required"] == true || x["renamedFrom"] != nil || x["movedFrom"] != nil {
			return true
		}
		for _, child := range x {
			if hasRequired(child) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if hasRequired(child) {
				return true
			}
		}
	}
	return false
}
