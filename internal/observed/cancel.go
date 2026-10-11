package observed

import (
	"context"
	"slices"
)

// checkEvery is how many points, spans or intervals a reducer takes between
// two checks of its context, and how long a run SortStableFunc sorts between
// two. A power of two, so the count is masked rather than divided.
const checkEvery = 1 << 10

// every is a loop's context check: ctx's error on its first call and on every
// checkEvery-th after it, nil between.
func every(ctx context.Context) func() error {
	n := 0
	return func() error {
		i := n
		n++
		if i&(checkEvery-1) != 0 {
			return nil
		}
		return ctx.Err()
	}
}

// SortStableFunc sorts s stably by cmp, as slices.SortStableFunc does,
// checking ctx before each run of checkEvery elements it sorts and every
// checkEvery elements it merges or moves, so a sort as long as a bucket's
// points stops when its caller does. It sorts the elements' indexes and moves
// each element once, so a large element is not copied into a second slice.
// Where ctx ends first it answers ctx's error and leaves s in no order, an
// element possibly in two places: the caller drops s.
func SortStableFunc[S ~[]E, E any](ctx context.Context, s S, cmp func(a, b E) int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := len(s)
	if n <= checkEvery {
		slices.SortStableFunc(s, cmp)
		return nil
	}
	check := every(ctx)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	byElement := func(a, b int) int { return cmp(s[a], s[b]) }
	for lo := 0; lo < n; lo += checkEvery {
		if err := ctx.Err(); err != nil {
			return err
		}
		slices.SortStableFunc(idx[lo:min(lo+checkEvery, n)], byElement)
	}
	// Merge the sorted runs pairwise, the left run's element first of two
	// equal ones, so the merge keeps the order stable.
	src, dst := idx, make([]int, n)
	for width := checkEvery; width < n; width *= 2 {
		for lo := 0; lo < n; lo += 2 * width {
			mid, hi := min(lo+width, n), min(lo+2*width, n)
			i, j := lo, mid
			for k := lo; k < hi; k++ {
				if err := check(); err != nil {
					return err
				}
				if j == hi || (i < mid && cmp(s[src[i]], s[src[j]]) <= 0) {
					dst[k], i = src[i], i+1
				} else {
					dst[k], j = src[j], j+1
				}
			}
		}
		src, dst = dst, src
	}
	// s[k] takes the element at s[src[k]]: follow each cycle of the
	// permutation, marking each place filled.
	for start := range src {
		if src[start] < 0 {
			continue
		}
		held := s[start]
		k := start
		for {
			if err := check(); err != nil {
				return err
			}
			from := src[k]
			src[k] = -1
			if from == start {
				s[k] = held
				break
			}
			s[k] = s[from]
			k = from
		}
	}
	return nil
}
