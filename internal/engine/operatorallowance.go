package engine

// One repository's allowance (decision 0314): the defaults file's entry for
// it under budget.repositories, else budget.centsPerDay. The file names a
// repository by its name or its id, and every key goes through the one
// lookup (repositoryByKey), so a former name inside its hold still finds the
// repository. A repository resolves its allowance at its first read and keeps
// it while it stays open, as the file itself is read once: a change to the
// file is a restart. Where two keys find one repository the lower holds, so
// a mistake in the file never gives away more. The start logs every key that
// finds no repository, finds one by a former name or finds one another key
// found too (checkOperatorAllowances); nothing is refused for it.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// repositoryAllowance is one repository's allowance, resolved once.
type repositoryAllowance struct {
	mu sync.Mutex
	// cents is the allowance in US cents a day; nil until a resolution
	// succeeded.
	cents *int64
}

// allowanceCents is the repository's allowance for the UTC day, in US cents.
// The caller holds an operator defaults file.
func (ds *dataset) allowanceCents(ctx context.Context) (int64, error) {
	budget := ds.svc.operatorDefaults.Budget
	if len(budget.Repositories) == 0 {
		return budget.CentsPerDay, nil
	}
	a := &ds.allowance
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cents != nil {
		return *a.cents, nil
	}
	cents := budget.CentsPerDay
	listed := false
	for _, key := range slices.Sorted(maps.Keys(budget.Repositories)) {
		repo, err := ds.svc.repositoryByKey(ctx, key)
		if errors.Is(err, substrate.ErrNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if repo.ID != ds.info.ID {
			continue
		}
		if !listed || budget.Repositories[key] < cents {
			cents = budget.Repositories[key]
		}
		listed = true
	}
	a.cents = &cents
	return cents, nil
}

// checkOperatorAllowances logs each budget.repositories key that finds no
// repository, finds one by a former name, or finds one another key found
// too, so a mistake in the file shows at the start.
func (s *service) checkOperatorAllowances(ctx context.Context) error {
	if s.operatorDefaults == nil {
		return nil
	}
	foundBy := map[string]string{}
	listed := s.operatorDefaults.Budget.Repositories
	for _, key := range slices.Sorted(maps.Keys(listed)) {
		repo, err := s.repositoryByKey(ctx, key)
		if errors.Is(err, substrate.ErrNotFound) {
			s.log.Warn("substrate: the operator defaults file's budget.repositories names a repository this server does not hold; it takes that allowance once it is registered under the name",
				"key", key)
			continue
		}
		if err != nil {
			return err
		}
		if key != repo.ID && key != repo.Authority {
			s.log.Warn("substrate: the operator defaults file's budget.repositories names a repository by a former name; write its name",
				"key", key, "name", repo.Authority)
		}
		if other, twice := foundBy[repo.ID]; twice {
			s.log.Warn("substrate: two keys of the operator defaults file's budget.repositories name one repository; the lower allowance holds",
				"key", key, "other", other, "name", repo.Authority)
		}
		foundBy[repo.ID] = key
	}
	return nil
}
