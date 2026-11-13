package appsetup

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/substrate"
)

type checkingInstall struct {
	Catalog
	expect catalog.InstallExpectations
}

func (c *checkingInstall) InstallApproved(_ context.Context, _ substrate.Actor, _ string, _ substrate.Dataset, expect catalog.InstallExpectations) (*catalog.Bundle, []substrate.SuggestedMapping, error) {
	c.expect = expect
	return nil, nil, substrate.ErrConflict
}

func TestSetupCarriesEveryApprovedCandidateToInstall(t *testing.T) {
	cat := &checkingInstall{}
	row := substrate.AuthorizationSetup{Package: &substrate.AuthorizationPackage{
		ID: "example.com/root", Shipped: 3,
		Publisher: &substrate.AuthorizationPackagePublisher{Digest: "root"},
		Requires: []substrate.AuthorizationPackageStep{
			{ID: "example.com/middle", Shipped: 2, Publisher: &substrate.AuthorizationPackagePublisher{Digest: "middle"}},
			{ID: "example.com/leaf", Shipped: 1, Publisher: &substrate.AuthorizationPackagePublisher{Digest: "leaf"}},
			{ID: "example.com/binary", Shipped: 4},
		},
	}}
	err := NewRunner(cat).setUpRow(context.Background(), newFakeDataset(), substrate.ActorPerson, pkgDetail(row.Package.ID, 1), row)
	if !errors.Is(err, substrate.ErrConflict) {
		t.Fatal(err)
	}
	if len(cat.expect) != 4 || cat.expect["example.com/root"].Digest != "root" ||
		cat.expect["example.com/middle"].Version != 2 || cat.expect["example.com/leaf"].Digest != "leaf" ||
		cat.expect["example.com/binary"] != nil {
		t.Fatalf("lost approval: %+v", cat.expect)
	}
}
