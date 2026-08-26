package e2e

import "sort"

// The extra cases: everything beyond the slice and the stories. Each group
// file registers its cases from init() into this table and owns one
// hundred-block of the order space, so files never edit each other and the
// run order stays deterministic whatever order the files compile in.
//
//	100 auth, tokens, rate limits, isolation
//	200 records, edges, merge/split, error shapes
//	300 queries, changelog, blobs
//	400 vocabulary upgrades, bundle lifecycle, GraphQL
//	500 functions, triggers, agents
type extraCase struct {
	order int
	id    string
	title string
	tests string
	fn    func(*C)
}

var extraCases []extraCase

func registerCase(order int, id, title, tests string, fn func(*C)) {
	extraCases = append(extraCases, extraCase{order: order, id: id, title: title, tests: tests, fn: fn})
}

// runExtraCases runs the registered table in order. The stories have already
// run: the repository holds the acme world, the story vocabulary and the
// agent threads, and extra cases may read all of it but must not break it.
func (r *run) runExtraCases() {
	// A duplicate id would split one case across two report entries, and a
	// duplicate order would leave the run order to compile order; both are
	// authoring mistakes, refused before anything runs.
	byID, byOrder := map[string]bool{}, map[int]string{}
	for _, ec := range extraCases {
		if byID[ec.id] {
			r.t.Fatalf("extra case %s is registered twice", ec.id)
		}
		byID[ec.id] = true
		if other, taken := byOrder[ec.order]; taken {
			r.t.Fatalf("extra cases %s and %s both claim order %d", other, ec.id, ec.order)
		}
		byOrder[ec.order] = ec.id
	}
	sort.SliceStable(extraCases, func(i, j int) bool { return extraCases[i].order < extraCases[j].order })
	for _, ec := range extraCases {
		r.runCase(ec.id, ec.title, ec.tests, ec.fn)
	}
}
