package engine

// The operator's install list (docs/plans/host-defaults.md): every package the
// defaults file names under `install:` is given to a repository through the
// ordinary published catalog install, at registration and at the first open
// under a file that lists a package the repository has not held. Whether it
// held one is read from its own changelog: a bundle record ever written for
// the package, even one later uninstalled, means the repository has had it,
// so an uninstall sticks, and because the changelog travels in the recovery
// export, a restore keeps it uninstalled too. This holds while the changelog
// keeps its whole history; a compaction would have to keep these entries.

import (
	"context"
	"sync"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// OperatorInstaller installs one published provider package into a
// repository: the catalog's install with no request behind it
// (catalog.SeedInstall).
type OperatorInstaller func(ctx context.Context, ds substrate.Dataset, pkg string) error

// WithOperatorInstaller gives the service the catalog install the operator's
// `install:` list runs through. Without it, or without a defaults file
// listing packages, nothing is installed.
func WithOperatorInstaller(fn OperatorInstaller) Option {
	return func(o *options) { o.operatorInstaller = fn }
}

// operatorInstallBudget bounds one repository's pass over the install list.
// The pass runs detached from the request that opened the repository, so a
// person leaving the page does not cut an install in half; a slow install
// still cannot hold the open forever.
const operatorInstallBudget = 2 * time.Minute

// operatorInstallFailures remembers, per repository and package, the last
// failed install's message, until the next attempt. It is memory only: a
// restart forgets it, and the first open after the restart tries again.
type operatorInstallFailures struct {
	mu sync.Mutex
	// byRepository maps a repository id to its failed packages' messages.
	byRepository map[string]map[string]string
}

func (f *operatorInstallFailures) set(repository, pkg, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byRepository == nil {
		f.byRepository = map[string]map[string]string{}
	}
	if msg == "" {
		delete(f.byRepository[repository], pkg)
		if len(f.byRepository[repository]) == 0 {
			delete(f.byRepository, repository)
		}
		return
	}
	if f.byRepository[repository] == nil {
		f.byRepository[repository] = map[string]string{}
	}
	f.byRepository[repository][pkg] = msg
}

// failure is the last failed install's message for one package, empty for
// none.
func (f *operatorInstallFailures) failure(repository, pkg string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byRepository[repository][pkg]
}

// runOperatorInstalls installs each package of the operator's list that ds's
// changelog has never held a bundle record for, in the file's order. A
// failure is logged, remembered for the operator defaults read, and tried
// again at the next open; it never fails the open or the registration that
// ran it.
func (s *service) runOperatorInstalls(ctx context.Context, ds *dataset) {
	op := s.operatorDefaults
	if op == nil || len(op.Install) == 0 || s.operatorInstaller == nil || s.readOnly {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), operatorInstallBudget)
	defer cancel()
	repo := ds.scope.Repository
	for _, pkg := range op.Install {
		held, err := ds.packageEverHeld(ctx, pkg)
		if err != nil {
			s.log.Warn("substrate: operator install: reading the changelog", "repository", repo, "package", pkg, "error", err)
			s.operatorInstalls.set(repo, pkg, refusalText(err))
			continue
		}
		if held {
			s.operatorInstalls.set(repo, pkg, "")
			continue
		}
		if err := s.operatorInstaller(ctx, ds, pkg); err != nil {
			s.log.Warn("substrate: operator install failed; the next open tries again", "repository", repo, "package", pkg, "error", err)
			s.operatorInstalls.set(repo, pkg, refusalText(err))
			continue
		}
		s.operatorInstalls.set(repo, pkg, "")
		s.log.Info("substrate: operator install", "repository", repo, "package", pkg)
	}
}

// packageEverHeld reports whether the repository's changelog holds any entry
// for the package's bundle record: an install, an upgrade or an uninstall.
func (ds *dataset) packageEverHeld(ctx context.Context, pkg string) (bool, error) {
	var held bool
	err := ds.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM changelog WHERE kind = $1 AND record_id = $2)`,
		kindBundle, pkg).Scan(&held)
	return held, err
}

// openForOperatorInstalls opens a repository just created, so the operator's
// install list runs inside the registration that created it. A failure to
// open is logged, and the first open after it runs the list.
func (s *service) openForOperatorInstalls(ctx context.Context, repo Repository) {
	if s.operatorDefaults == nil || len(s.operatorDefaults.Install) == 0 || s.operatorInstaller == nil {
		return
	}
	if _, err := s.open(ctx, repo); err != nil {
		s.log.Warn("substrate: opening a new repository for the operator's installs", "repository", repo.ID, "error", err)
	}
}
