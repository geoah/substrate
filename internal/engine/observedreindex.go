package engine

import (
	"context"
	"fmt"
)

// ReindexReport is what `repository reindex --observed` queued.
type ReindexReport struct {
	Repository string   `json:"repository"`
	Kinds      []string `json:"kinds"`
	Records    int64    `json:"records"`
}

// ReindexObserved queues the background re-derivation of a repository's
// observed index: every row of every kind that binds core's `observed` and
// holds a row, in index_reprojections, the work list a vocabulary change and
// a rules change use. It is the operator's repair short of a rebuild, for an
// index a fault left behind its records; it writes no changelog entry and
// re-derives no row itself. Aggregate reads over those kinds are refused as
// unavailable until the pass has finished (checkObservedReady). The pass
// starts in this process, and an open of the repository resumes it where a
// close stopped it.
func (s *service) ReindexObserved(ctx context.Context, repository string) (ReindexReport, error) {
	if s.readOnly {
		return ReindexReport{}, ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return ReindexReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return ReindexReport{}, err
	}
	report := ReindexReport{Repository: repo.ID}
	err = ds.inRawTx(ctx, func(t *txn) error {
		kinds, err := t.observedKindsHoldingRows()
		if err != nil {
			return err
		}
		owed := make([]indexReprojection, len(kinds))
		for i, kind := range kinds {
			owed[i] = indexReprojection{kind: kind, observed: indexFilter{moved: true, every: true}}
		}
		if err := t.requestReprojections(owed); err != nil {
			return err
		}
		report.Kinds = kinds
		return t.row(`SELECT count(*) FROM records WHERE kind = ANY($1::text[])`, kinds).Scan(&report.Records)
	})
	if err != nil {
		return report, fmt.Errorf("substrate/engine: repository %s: queue the observed index re-derivation: %w", repo.ID, err)
	}
	if len(report.Kinds) > 0 {
		ds.kickIndexReprojection()
	}
	return report, nil
}
