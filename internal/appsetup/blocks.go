package appsetup

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

// A block's sentence is the server's one wording of it, which both of the
// approval page's views show. It never names a package: the row's title does,
// from the package's id, and the catalog carries no other name.

// block is a row's block as the wire carries it.
func block(reason, problem, fix string) *substrate.AuthorizationBlock {
	return &substrate.AuthorizationBlock{Reason: reason, Problem: problem, Fix: fix}
}

// packageProblem is why a package cannot be installed or upgraded, worded
// twice: itself for the package's own row or step, requirer for the row of a
// package that needs it first, which the block blocks too.
type packageProblem struct {
	reason, fix, itself, requirer string
}

// own is the block as the package's own row or step says it.
func (p *packageProblem) own() *substrate.AuthorizationBlock {
	if p == nil {
		return nil
	}
	return block(p.reason, p.itself, p.fix)
}

// needsFirst is the block as the row of a package that needs it first says
// it.
func (p *packageProblem) needsFirst() *substrate.AuthorizationBlock {
	if p == nil {
		return nil
	}
	return block(p.reason, p.requirer, p.fix)
}

func notShipped() *packageProblem {
	return &packageProblem{
		reason:   substrate.BlockNotShipped,
		itself:   "This server does not offer this package, so approving cannot set it up.",
		requirer: "It needs a package first that this server does not offer, so approving cannot set it up.",
	}
}

// sample is a sample package, which the console's own door imports with its
// requirements.
func sample() *packageProblem {
	return &packageProblem{
		reason: substrate.BlockSample, fix: "/data",
		itself:   "This is a sample, which you add yourself from All data.",
		requirer: "It needs a sample first, which you add yourself from All data.",
	}
}

func quarantined(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockQuarantined, fix: ProviderPage(id),
		itself:   "Your copy of this package is quarantined, so approving cannot set it up. Its provider page says why.",
		requirer: "Your copy of a package it needs first is quarantined, so approving cannot set it up. Its provider page says why.",
	}
}

func declaredByOwner(id string, held int64) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockDeclaredByOwner, fix: ProviderPage(id),
		itself:   fmt.Sprintf("You declared this package yourself, at version %d. Installing the published version would replace yours.", held),
		requirer: fmt.Sprintf("It needs a package first that you declared yourself, at version %d. Installing the published version would replace yours.", held),
	}
}

func serverUpdate(shipped, need int64) *packageProblem {
	return &packageProblem{
		reason:   substrate.BlockServerUpdate,
		itself:   fmt.Sprintf("This server ships version %d; the app needs version %d. The server needs an update first.", shipped, need),
		requirer: fmt.Sprintf("It needs a package first at version %d, and this server ships version %d. The server needs an update first.", need, shipped),
	}
}

func upgradeRefused(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		itself:   "Updating this package is refused for now. Its provider page says why.",
		requirer: "Updating a package it needs first is refused for now. Its provider page says why.",
	}
}

func upgradeConfirm(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		itself:   "Updating this package would change records in a way its provider page asks you to confirm first.",
		requirer: "Updating a package it needs first would change records in a way its provider page asks you to confirm first.",
	}
}

func previewFailed(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		itself:   "Updating this package could not be previewed. The server log says why.",
		requirer: "Updating a package it needs first could not be previewed. The server log says why.",
	}
}
