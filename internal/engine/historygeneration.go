package engine

import (
	"context"
	"fmt"
)

// A repository's history generation is minted when its row is written, so an
// import into a database with no row for it rotates by construction. A
// database restored from a DUMP keeps the row, and with it the generation the
// dump held, while the changelog it carries may be shorter than the one
// clients saved cursors against. Nothing in the tables can tell that restore
// from an ordinary restart, so the operator says so: `substratectl repository
// rotate-generation` is the step in the restore procedure that resets every
// saved change cursor once (decision 0056).

// RotateReport is what one rotation did.
type RotateReport struct {
	Repository string `json:"repository"`
	Previous   string `json:"previous"`
	Generation string `json:"generation"`
}

// GenerationRotator is the operator hat's rotation seam, off
// substrate.Service like Rebuilder and asserted here for the same reason.
type GenerationRotator interface {
	RotateHistoryGeneration(ctx context.Context, repository string) (RotateReport, error)
}

var _ GenerationRotator = (*service)(nil)

// RotateHistoryGeneration mints a new history generation for one repository
// and stores it on the row, so every cursor saved under the old one is refused
// at its next resume and re-lists. It opens the repository the way a rebuild
// does, so a running server, which holds the directory lock and has the old
// generation cached, refuses it rather than serve two generations at once.
func (s *service) RotateHistoryGeneration(ctx context.Context, repository string) (RotateReport, error) {
	if s.readOnly {
		return RotateReport{}, ErrDirectoryReadOnly
	}
	repo, err := s.repositoryByID(ctx, repository)
	if err != nil {
		return RotateReport{}, err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return RotateReport{}, err
	}
	report := RotateReport{Repository: repo.ID, Previous: repo.HistoryGeneration}
	if err := ds.directoryErr(); err != nil {
		return report, err
	}
	generation, err := newHistoryGeneration()
	if err != nil {
		return report, err
	}
	if _, err := s.maint.ExecContext(ctx,
		`UPDATE repositories SET history_generation = $1 WHERE id = $2`, generation, repo.ID); err != nil {
		return report, fmt.Errorf("substrate/engine: rotate the history generation of %s: %w", repo.ID, err)
	}
	ds.mu.Lock()
	ds.generation = generation
	ds.mu.Unlock()
	report.Generation = generation
	return report, nil
}

// historyGeneration is the dataset's cached generation, read under the lock a
// rotation takes to replace it.
func (ds *dataset) historyGeneration() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.generation
}
