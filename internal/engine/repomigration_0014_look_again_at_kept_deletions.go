package engine

// Repository migration 14: the forgetting looks again at the deletions it
// passed by (decision 0319).
//
// Before this release the compaction pass never forgot the deletion of a
// record a package ships: it passed such a record by and moved its
// forgetting's mark past it, leaving the record's whole history. The pass
// now forgets such a record's own entries but its deletion's line, which
// it keeps with the hands that wrote the record and no value, and a bundle
// record's the same way. The deletions it passed by lie below its
// mark, which it never reads under again, so the mark goes back to the
// start once: the next pass reads the deletions again, keeps the shipped
// ones, finds the ones it forgot already gone and any kept one in its kept
// form, and moves the mark on.
//
// It writes no record and no row of the fold: the mark is the pass's own
// progress (compaction_state.forgot_upto), which no rebuild and no archive
// holds, and which a restore or an import starts at the beginning anyway.
// Run again, it costs the pass one more look and changes nothing else.

import "fmt"

func migrateLookAgainAtKeptDeletions(t *txn) (string, error) {
	res, err := t.exec(`UPDATE compaction_state SET forgot_upto = 0, updated_at = now() WHERE forgot_upto > 0`)
	if err != nil {
		return "", fmt.Errorf("lower the forgetting's mark: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return "", err
	} else if n == 0 {
		return "the forgetting had looked at no deletion yet", nil
	}
	return "the forgetting looks again at every deletion it passed", nil
}
