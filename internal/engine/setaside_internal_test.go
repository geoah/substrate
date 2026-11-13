package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A failure no repository's absence would cure is the start's to stop on:
// setAside hands it back and sets nothing aside, before it asks the database
// anything.
func TestAFailureOfTheServersOwnIsNotSetAside(t *testing.T) {
	t.Parallel()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, tc := range map[string]struct {
		ctx context.Context
		err error
	}{
		"the start canceled": {canceled, errors.New("substrate/engine: boot check: interrupted")},
		"a deadline":         {context.Background(), fmt.Errorf("boot check: %w", context.DeadlineExceeded)},
		"another writer":     {context.Background(), fmt.Errorf("boot check: %w", ErrRepositoryHasAnotherWriter)},
		"the lease lost":     {context.Background(), fmt.Errorf("boot check: %w", ErrWriterLeaseLost)},
	} {
		s := &service{}
		repo := Repository{ID: "rnd-0000000000000000000000000", Authority: "ada.example.com"}
		if err := s.setAside(tc.ctx, rowSetAside(repo, setAsideAtCheck, tc.err)); !errors.Is(err, tc.err) {
			t.Errorf("%s: setAside = %v, want the failure back", name, err)
		}
		if n := s.aside.count(); n != 0 {
			t.Errorf("%s: %d repositories set aside, want none", name, n)
		}
	}
}

// A set-aside repository is found by its id or its name and refused with the
// reason a client acts on; an empty argument finds nothing. A directory no row
// was proven to be only claims its name: it keeps a registration off it and
// takes no repository of that name down with it.
func TestASetAsideRepositoryIsRefusedByItsIDOrItsName(t *testing.T) {
	t.Parallel()
	s := &service{}
	s.aside.add(rowSetAside(Repository{ID: "rnd-a", Authority: "ada.example.com"}, setAsideAtCheck, errors.New("damaged")))
	s.aside.add(&setAsideRepository{keys: []string{"rnd-c"}, authority: "bob.example.com", claim: true, stage: setAsideAtImport})
	for _, at := range [][2]string{{"rnd-a", ""}, {"", "ada.example.com"}, {"rnd-a", "ada.example.com"}, {"rnd-c", ""}} {
		if err := s.refuseSetAside(at[0], at[1]); !errors.Is(err, substrate.ErrSetAside) {
			t.Errorf("refuseSetAside(%q, %q) = %v, want ErrSetAside", at[0], at[1], err)
		}
	}
	for _, at := range [][2]string{{"", ""}, {"rnd-b", "bob.example.com"}, {"", "bob.example.com"}} {
		if err := s.refuseSetAside(at[0], at[1]); err != nil {
			t.Errorf("refuseSetAside(%q, %q) = %v, want nil", at[0], at[1], err)
		}
	}
	if !s.aside.holdsName("bob.example.com") || !s.aside.holdsName("ada.example.com") || s.aside.holdsName("cat.example.com") {
		t.Error("the names held are not the two set aside")
	}
}
