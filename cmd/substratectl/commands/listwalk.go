package commands

import "errors"

// walkList pages a list to its end, page by page, through fetch, which reads
// the page after the cursor it is given ("" for the first) and returns the
// next cursor ("" once the list is exhausted). A continuation the server can
// no longer resume (410 compacted) starts the walk again from the first page,
// ONCE: reset drops what the walk collected, so nothing is kept twice, and a
// second refusal is returned as it is, so a server that keeps refusing is a
// failure, never a loop. Any other refusal of a continuation (a 422 for
// another filter, a 404 for a kind that went) is returned as it is too. It
// reports whether the walk reached the end within maxPages.
func walkList(maxPages int, reset func(), fetch func(after string) (string, error)) (bool, error) {
	after, restarted := "", false
	for pages := 0; pages < maxPages; {
		next, err := fetch(after)
		if err != nil {
			if restarted && compactedCursor(err) {
				return false, listRestartRefused(err)
			}
			if after == "" || restarted || !compactedCursor(err) {
				return false, err
			}
			restarted, after, pages = true, "", 0
			reset()
			continue
		}
		pages++
		if next == "" {
			return true, nil
		}
		after = next
	}
	return false, nil
}

// compactedCursor reports whether err is the server's 410 compacted: a cursor
// or a position it can no longer resume.
func compactedCursor(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Code == "compacted"
}

// listCursorRefused is a refused --after as the error the command fails
// with. One page is all the command reads, so starting the listing again is
// the caller's: what it kept of the earlier pages goes with the cursor.
func listCursorRefused(err error) error {
	var ae *apiError
	if errors.As(err, &ae) {
		ae.Headline = "the server can no longer resume the --after cursor"
		ae.Hint = "list again from the first page: run the same command without --after, and drop the cursor and the rows read with it"
	}
	return err
}

// listRestartRefused is a walk's second refusal: the server could not resume
// the listing it started again either, which is a failure, shown as one.
func listRestartRefused(err error) error {
	var ae *apiError
	if errors.As(err, &ae) {
		ae.Headline = "the server could not resume this listing, even after it started again from the first page"
		ae.Hint = "try again in a moment; if it keeps failing, the server's operator should read its log"
	}
	return err
}
