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
// twice: own for the package's own row or step, first for the row of a
// package that needs it installed first, which the block blocks too.
type packageProblem struct {
	reason, fix, own, first string
}

// of is the block as the row of the package says it, or (first) as the row
// that needs it first says it.
func (p *packageProblem) of(first bool) *substrate.AuthorizationBlock {
	if p == nil {
		return nil
	}
	if first {
		return block(p.reason, p.first, p.fix)
	}
	return block(p.reason, p.own, p.fix)
}

func notShipped() *packageProblem {
	return &packageProblem{
		reason: substrate.BlockNotShipped,
		own:    "This server does not offer this package, so approving cannot set it up.",
		first:  "It needs a package first that this server does not offer, so approving cannot set it up.",
	}
}

// sample is a sample package, which the console's own door imports with its
// requirements.
func sample() *packageProblem {
	return &packageProblem{
		reason: substrate.BlockSample, fix: "/data",
		own:   "This is a sample, which you add yourself from All data.",
		first: "It needs a sample first, which you add yourself from All data.",
	}
}

func quarantined(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockQuarantined, fix: ProviderPage(id),
		own:   "Your copy of this package is quarantined, so approving cannot set it up. Its provider page says why.",
		first: "Your copy of a package it needs first is quarantined, so approving cannot set it up. Its provider page says why.",
	}
}

func declaredByOwner(id string, held int64) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockDeclaredByOwner, fix: ProviderPage(id),
		own:   fmt.Sprintf("You declared this package yourself, at version %d. Installing the published version would replace yours.", held),
		first: fmt.Sprintf("It needs a package first that you declared yourself, at version %d. Installing the published version would replace yours.", held),
	}
}

func serverUpdate(shipped, need int64) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockServerUpdate,
		own:    fmt.Sprintf("This server ships version %d; the app needs version %d. The server needs an update first.", shipped, need),
		first:  fmt.Sprintf("It needs a package first at version %d, and this server ships version %d. The server needs an update first.", need, shipped),
	}
}

func upgradeRefused(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		own:   "Updating this package is refused for now. Its provider page says why.",
		first: "Updating a package it needs first is refused for now. Its provider page says why.",
	}
}

func upgradeConfirm(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		own:   "Updating this package would change records in a way its provider page asks you to confirm first.",
		first: "Updating a package it needs first would change records in a way its provider page asks you to confirm first.",
	}
}

func previewFailed(id string) *packageProblem {
	return &packageProblem{
		reason: substrate.BlockUpgradeNeedsReview, fix: ProviderPage(id),
		own:   "Updating this package could not be previewed. The server log says why.",
		first: "Updating a package it needs first could not be previewed. The server log says why.",
	}
}
