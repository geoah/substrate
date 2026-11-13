package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

type reviewDataset struct {
	substrate.Dataset
	installed []substrate.BundleInstall
}

func (*reviewDataset) Repository() substrate.RepositoryInfo {
	return substrate.RepositoryInfo{Authority: "owner.example"}
}

func (*reviewDataset) Get(context.Context, string, string) (*substrate.Record, error) {
	return nil, substrate.ErrNotFound
}

func (*reviewDataset) PlanBundleUpgrade(context.Context, []map[string]any) (substrate.BundleUpgrade, error) {
	return substrate.BundleUpgrade{Available: true}, nil
}

func (d *reviewDataset) InstallBundleClosure(_ context.Context, _ substrate.Actor, _ []map[string]any, _ []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	d.installed = append(d.installed, opts)
	return nil, nil
}

func TestPublishedInstallIsBoundToReviewedCandidate(t *testing.T) {
	ctx := context.Background()
	old, _ := publishedFixture(t, 1)
	ds := &reviewDataset{}
	preview, err := old.Upgrade(ctx, "example.com/notes", ds, nil)
	if err != nil || preview.CandidateVersion != 1 || len(preview.CandidateDigest) != 64 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	next, _ := publishedFixture(t, 1, 2)
	expect := &PublishedCandidate{Version: preview.CandidateVersion, Digest: preview.CandidateDigest}
	for _, candidate := range []*PublishedCandidate{expect, {Version: 2, Digest: preview.CandidateDigest}} {
		if _, _, err := next.InstallReviewed(ctx, substrate.ActorConsole, "example.com/notes", ds, nil, candidate); !errors.Is(err, substrate.ErrConflict) || !strings.Contains(err.Error(), "the publisher listed another version since you reviewed this one; review it again") {
			t.Fatalf("changed candidate accepted: %v", err)
		}
	}
	if len(ds.installed) != 0 {
		t.Fatal("a stale review installed code")
	}
	preview, err = next.Upgrade(ctx, "example.com/notes", ds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := next.InstallReviewed(ctx, substrate.ActorConsole, "example.com/notes", ds, nil, &PublishedCandidate{Version: preview.CandidateVersion, Digest: preview.CandidateDigest}); err != nil {
		t.Fatal(err)
	}
	if len(ds.installed) != 1 || ds.installed[0].PublishedDigest != preview.CandidateDigest {
		t.Fatalf("installed %+v", ds.installed)
	}
	if _, _, err := next.InstallConfirmed(ctx, substrate.ActorConsole, "example.com/notes", ds, nil); err != nil {
		t.Fatal(err)
	}
	if len(ds.installed) != 2 {
		t.Fatal("legacy install failed")
	}
}

func (*reviewDataset) BundleStatusesUncounted(context.Context) ([]substrate.BundleStatus, error) {
	return nil, nil
}

func TestApprovedInstallBindsEveryDependencyBeforeWriting(t *testing.T) {
	ctx := context.Background()
	old, _ := publishedFixture(t, 1)
	reviewed, err := old.Resolve(ctx, "example.com/notes", 0)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := publishedFixture(t, 1, 2)
	root, err := ParseClosure([]byte(requiringClosure("example.com/root", 1, "example.com/safe", 1, "example.com/notes", 1)), "example.com/root", 1)
	if err != nil {
		t.Fatal(err)
	}
	safe, err := ParseClosure([]byte(requiringClosure("example.com/safe", 1)), "example.com/safe", 1)
	if err != nil {
		t.Fatal(err)
	}
	root.KeptBy, safe.KeptBy = substrate.KeptByPublisher, substrate.KeptByPublisher
	next.byID[root.ID], next.byID[safe.ID] = root, safe
	current, err := next.Resolve(ctx, reviewed.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []*PublishedCandidate{
		{Version: reviewed.Version, Digest: reviewed.origin.SHA256},
		{Version: current.Version, Digest: reviewed.origin.SHA256},
		nil,
	} {
		ds := &reviewDataset{}
		expect := InstallExpectations{root.ID: nil, safe.ID: nil, reviewed.ID: want}
		if _, _, err := next.InstallApproved(ctx, substrate.ActorConsole, root.ID, ds, expect); !errors.Is(err, substrate.ErrConflict) {
			t.Fatalf("changed dependency accepted: %v", err)
		}
		if len(ds.installed) != 0 {
			t.Fatal("even the unchanged first dependency must not land before validation")
		}
	}
	ds := &reviewDataset{}
	expect := InstallExpectations{root.ID: nil, safe.ID: nil, current.ID: {Version: current.Version, Digest: current.origin.SHA256}}
	if _, _, err := next.InstallApproved(ctx, substrate.ActorConsole, root.ID, ds, expect); err != nil {
		t.Fatal(err)
	}
	if len(ds.installed) != 3 || ds.installed[1].PublishedDigest != current.origin.SHA256 {
		t.Fatalf("installed %+v", ds.installed)
	}
	delete(expect, safe.ID)
	ds.installed = nil
	if _, _, err := next.InstallApproved(ctx, substrate.ActorConsole, root.ID, ds, expect); !errors.Is(err, substrate.ErrConflict) || len(ds.installed) != 0 {
		t.Fatalf("unapproved dependency installed: %v", err)
	}
}
