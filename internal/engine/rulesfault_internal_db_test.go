package engine

// A READ OF THE RULES THAT CANNOT BE MADE REFUSES (decision 0276: a failed
// read is an error, never a wider answer). The readers of the rules (a
// person's roles, an app's grant, the holders of a piece of work, the actor
// kind beneath the roles) fail through one seam (WithTestRulesFault), and
// every boundary that reads them is walked with them failing: each read,
// each write door, each continuation, each piece of work read back from its
// record. Every one refuses, and none answers. The table is held to the
// code: a function that calls a reader of the rules and has no row fails the
// test, as does a row whose function no longer reads them.

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// errRulesFault is what the failing reads answer.
var errRulesFault = errors.New("the rules' database did not answer")

// ruleReaders are the functions that read the rules a check holds a hand to,
// or that pass such a read on. A function that calls one and is not one is a
// boundary, and has its row in ruleBoundaries.
var ruleReaders = []string{
	"rolesIn", "rolesOf", "appGrants", "runReads", "heldScope", "readsAllow", "heldReads", "hostReadsHeld", "pinnedAnswer",
	"heldBy", "settlingHeld", "actorKeyOfID", "itemHolders", "requestHolders",
	"mayContinue", "continuation", "authorizeWrite", "handWrites", "mayWrite", "behalfMayWrite", "PersonRoles",
}

// faulted reports whether err is the failing read's.
func faulted(err error) bool {
	return err != nil && strings.Contains(err.Error(), errRulesFault.Error())
}

func (r *runsRepository) fail(whats ...string) { r.r.failRules(errRulesFault, whats...) }

func (r *runsRepository) heal() { r.r.failRules(nil) }

// ada is the context of the admin's own request.
func (r *runsRepository) adaCtx() context.Context { return r.person(r.adaID, substrate.ActorPerson) }

// toolRefused runs one turn of agent for ada with the reads failing, calling
// call, and reports whether its one tool row was refused by the failed read.
func (r *runsRepository) toolRefused(t *testing.T, agent string, call fakeCall, whats ...string) (bool, string) {
	t.Helper()
	r.fail(whats...)
	defer r.heal()
	_, rows := r.chat(t, r.adaCtx(), substrate.ActorPerson, agent, "", fakeTurn{calls: []fakeCall{call}}, fakeTurn{content: "done."})
	if len(rows) != 1 {
		return false, fmt.Sprint(rows)
	}
	content := fmt.Sprint(rows[0]["content"])
	return rows[0]["ok"] == false && strings.Contains(content, errRulesFault.Error()), content
}

// errRefused reports a call that returned the failing read's error.
func errRefused(err error) (bool, string) { return faulted(err), fmt.Sprint(err) }

// seededNotes writes the notes the rows read, as ada.
func (r *runsRepository) seededNotes(t *testing.T) {
	t.Helper()
	for _, id := range []string{"n1", "n2"} {
		if _, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid(id), Properties: map[string]any{"name": "seen"}}); err != nil {
			t.Fatal(err)
		}
	}
}

// staged is one of ada's turns of agent, done with the reads healthy.
func (r *runsRepository) staged(t *testing.T, agent string, turns ...fakeTurn) string {
	t.Helper()
	thread, _ := r.chat(t, r.adaCtx(), substrate.ActorPerson, agent, "", turns...)
	return thread
}

var ruleBoundaries = map[string]func(t *testing.T, r *runsRepository) (bool, string){
	// Reads.
	"agentloop.go:dispatchQuery": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "scribe", fakeCall{"query", toolArgs(t, map[string]any{"kind": r.note})})
	},
	"agentloop.go:dispatchPropose": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "proposer", fakeCall{"propose", toolArgs(t, map[string]any{
			"kind": r.note, "target": fid("n1"), "diff": map[string]any{"properties": map[string]any{"name": "x"}}, "rationale": "x",
		})})
	},
	"agentedit.go:dispatchEdit": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "editor", fakeCall{"edit", toolArgs(t, map[string]any{"op": "view", "kind": r.note, "id": fid("n1"), "property": "name"})})
	},
	"applets.go:dispatchAppletPreview": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "previewer", fakeCall{"preview", toolArgs(t, map[string]any{"id": "an-applet"})})
	},
	"agentpinned.go:takePinned": pinnedRefused,
	// The cached branch, asked on the grant of the app whose work took the
	// block: no model request of the resume carries it.
	"agentpinned.go:continuePinned": func(t *testing.T, r *runsRepository) (bool, string) {
		_, ask, _ := r.pinnedAppAsk(t, "pinned-in-the-table")
		r.fail("grant")
		defer r.heal()
		reqs := r.answerAndResume(t, ask)
		for _, req := range reqs {
			if requestCarries(req, "pinned-in-the-table") {
				return false, "the resume replayed the block"
			}
		}
		return len(reqs) > 0, fmt.Sprintf("%d requests", len(reqs))
	},
	"agentmentions.go:stampMentions": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		ghost := "[ghost](substrate://" + vocabulary.RecordPath(r.note, fid("ghost")) + ")"
		thread, _ := r.chat(t, r.adaCtx(), substrate.ActorPerson, "scribe", "", fakeTurn{content: "see " + ghost})
		var mentions string
		if err := r.ds.db.QueryRowContext(context.Background(), `SELECT coalesce(props->>'mentions', '') FROM records
			WHERE kind = $1 AND `+referencePathSQL("props", msgRelThread)+` = $2 AND props->>'role' = 'assistant'
			ORDER BY created_at DESC LIMIT 1`, typeMessage, vocabulary.RecordPath(typeThread, thread)).Scan(&mentions); err != nil {
			return false, err.Error()
		}
		return mentions != "" && !strings.Contains(mentions, `"missing"`), mentions
	},
	"agentturncontext.go:contextRecord": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		r.fake.script("scribe", fakeTurn{content: "done."})
		_, err := r.ds.ChatAgent(r.adaCtx(), substrate.ActorPerson, r.pkg+"/scribe", substrate.ChatInput{
			Message: "about this", Context: []string{vocabulary.RecordPath(r.note, fid("n1"))},
		}, nil)
		if err != nil {
			return errRefused(err)
		}
		reqs := r.fake.requestsOf("scribe")
		return len(reqs) > 0 && !requestCarries(reqs[len(reqs)-1], fid("n1")), "the turn carried the context record"
	},
	"runner.go:callHostFunction": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, _, err := r.ds.CallFunction(r.adaCtx(), substrate.ActorPerson, vocabulary.HostFunctionQuery, map[string]any{"kind": r.note})
		return errRefused(err)
	},
	"runner.go:Get":       hostReadRefused("get"),
	"runner.go:List":      hostReadRefused("list"),
	"runner.go:Search":    hostReadRefused("search"),
	"runner.go:Aggregate": hostReadRefused("aggregate"),

	// Write doors.
	"write.go:putSpec": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: r.note, ID: fid("n3"), Properties: map[string]any{"name": "x"}})
		return errRefused(err)
	},
	"write.go:patch": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, err := r.ds.Patch(r.adaCtx(), substrate.ActorPerson, r.note, fid("n1"), substrate.PatchInput{Properties: map[string]any{"name": "x"}})
		return errRefused(err)
	},
	"write.go:softDeleteIf": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, err := r.ds.Delete(r.adaCtx(), substrate.ActorPerson, r.note, fid("n1"), substrate.DeleteInput{})
		return errRefused(err)
	},
	"write.go:putBounded": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "scribe", fakeCall{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})})
	},
	"write.go:patchBounded": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "scribe", fakeCall{"write", writeArgs(t, "patch", r.note, fid("n1"), map[string]any{"name": "x"})})
	},
	"write.go:deleteBounded": func(t *testing.T, r *runsRepository) (bool, string) {
		return r.toolRefused(t, "scribe", fakeCall{"write", writeArgs(t, "delete", r.note, fid("n1"), nil)})
	},
	"merge.go:mergeRecordIf": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, err := r.ds.Merge(r.adaCtx(), substrate.ActorPerson, substrate.MergeInput{Kind: r.note, Winner: fid("n1"), Loser: fid("n2")})
		return errRefused(err)
	},
	"policy.go:putGatedRequest": func(t *testing.T, r *runsRepository) (bool, string) {
		ty, err := r.ds.resolveType(r.note)
		if err != nil {
			return false, err.Error()
		}
		r.fail("roles")
		defer r.heal()
		err = r.ds.inTx(r.adaCtx(), substrate.ActorPerson, false, func(tx *txn) error {
			_, err := tx.putGatedRequest(&gatedWrite{op: policyOpPatch, kind: ty, id: fid("n1"), props: map[string]any{"name": "x"}, key: "gated"})
			return err
		})
		return errRefused(err)
	},
	"agentvocabulary.go:dispatchVocabulary": func(t *testing.T, r *runsRepository) (bool, string) {
		pkg := r.ds.Repository().Authority + "/declared"
		return r.toolRefused(t, "declarer", fakeCall{"vocabulary", toolArgs(t, map[string]any{"documents": []any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "thing"}, map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}),
		}})})
	},
	"roles.go:requestMayWrite": func(t *testing.T, r *runsRepository) (bool, string) {
		ty, err := r.ds.resolveType(r.note)
		if err != nil {
			return false, err.Error()
		}
		r.fail("roles")
		defer r.heal()
		return errRefused(r.ds.requestMayWrite(r.adaCtx(), substrate.ActorPerson, ty))
	},
	// The admin's check a delete and a rename both take.
	"auth.go:requireAdmin": func(t *testing.T, r *runsRepository) (bool, string) {
		repo, err := r.r.s.repositoryByKey(context.Background(), r.ada)
		if err != nil {
			return false, err.Error()
		}
		r.fail("roles")
		defer r.heal()
		if refused, saw := errRefused(r.r.s.mayDeleteRepository(context.Background(), repo, r.adaID)); !refused {
			return false, "the delete: " + saw
		}
		return errRefused(r.r.s.requireAdmin(context.Background(), repo, r.adaID, "renaming a repository is an admin's"))
	},
	"auth.go:rolesHeldIn": func(t *testing.T, r *runsRepository) (bool, string) {
		repo, err := r.r.s.repositoryByKey(context.Background(), r.ada)
		if err != nil {
			return false, err.Error()
		}
		r.fail("roles")
		defer r.heal()
		_, err = r.r.s.rolesHeldIn(context.Background(), repo, r.adaID)
		return errRefused(err)
	},
	"people.go:setPersonRoles": personRolesRefused,
	"people.go:adminsBut":      personRolesRefused,

	// Continuations.
	"agents.go:ChatAgent": func(t *testing.T, r *runsRepository) (bool, string) {
		thread := r.staged(t, "scribe", fakeTurn{content: "first."})
		r.fail("holders")
		defer r.heal()
		bo := substrate.PersonActor(r.boID)
		_, err := r.ds.ChatAgent(r.person(r.boID, bo), bo, r.pkg+"/scribe", substrate.ChatInput{Message: "mine now", Thread: thread}, nil)
		return errRefused(err)
	},
	"agents.go:StopAgentTurn": func(t *testing.T, r *runsRepository) (bool, string) {
		thread := r.staged(t, "scribe", fakeTurn{content: "first."})
		r.fail("holders")
		defer r.heal()
		bo := substrate.PersonActor(r.boID)
		_, err := r.ds.StopAgentTurn(r.person(r.boID, bo), bo, thread)
		return errRefused(err)
	},

	// Work read back from its record.
	"agentdecision.go:continueThread": func(t *testing.T, r *runsRepository) (bool, string) {
		thread := r.staged(t, "scribe", fakeTurn{content: "first."})
		var row string
		if err := r.ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
			var err error
			row, err = tx.putThreadSystemRowID(thread, map[string]any{"event": "recordResolved"}, nil,
				map[string]any{"onBehalfOf": vocabulary.ActorRef(actorID(substrate.ActorPerson))})
			return err
		}); err != nil {
			return false, err.Error()
		}
		before := len(r.fake.requestsOf("scribe"))
		r.fail("holders")
		defer r.heal()
		r.fake.script("scribe", fakeTurn{content: "resumed."})
		r.ds.continueThread(context.Background(), thread, row)
		return len(r.fake.requestsOf("scribe")) == before, "the thread resumed"
	},
	"agentdecision.go:recordResolution": func(t *testing.T, r *runsRepository) (bool, string) {
		req := r.proposed(t)
		r.fail("holders")
		defer r.heal()
		_, err := r.ds.Patch(context.Background(), substrate.ActorSystem, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
			Properties: map[string]any{"decision": decisionAccepted}, IfVersion: &req.Version,
		})
		return errRefused(err)
	},
	"agentdecision.go:recordProposalConflict": func(t *testing.T, r *runsRepository) (bool, string) {
		req := r.proposed(t)
		r.fail("holders")
		defer r.heal()
		return errRefused(r.ds.inTx(context.Background(), substrate.ActorSystem, true, func(tx *txn) error {
			row, err := tx.loadRow(eref{Kind: vocabulary.KindRecordPatchRequest, ID: req.ID}, false)
			if err != nil {
				return err
			}
			return tx.recordProposalConflict(row, "the target moved")
		}))
	},
	"agentjobs.go:runJob": func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("holders")
		defer r.heal()
		r.fake.script("digger", fakeTurn{content: "dug."})
		r.chat(t, r.adaCtx(), substrate.ActorPerson, "boss", "",
			fakeTurn{calls: []fakeCall{{"digger", toolArgs(t, map[string]any{"input": "dig"})}}}, fakeTurn{content: "started."})
		time.Sleep(500 * time.Millisecond)
		return len(r.fake.requestsOf("digger")) == 0, "the job ran"
	},
	"agentjobs.go:settleInterruptedJobs": func(t *testing.T, r *runsRepository) (bool, string) {
		thread := r.staged(t, "scribe", fakeTurn{calls: []fakeCall{{"query", toolArgs(t, map[string]any{"kind": r.note})}}}, fakeTurn{content: "done."})
		res, err := r.ds.db.ExecContext(context.Background(), `UPDATE records SET props = jsonb_set(props, '{job}', '{"state":"working"}')
			WHERE kind = $1 AND props->>'role' = 'tool' AND `+referencePathSQL("props", msgRelThread)+` = $2`,
			typeMessage, vocabulary.RecordPath(typeThread, thread))
		if err != nil {
			return false, err.Error()
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return false, fmt.Sprintf("staged %d working jobs, want 1", n)
		}
		r.fail("holders")
		defer r.heal()
		return errRefused(r.ds.settleInterruptedJobs(context.Background()))
	},
	"agents.go:settleInterruptedThreads": func(t *testing.T, r *runsRepository) (bool, string) {
		r.runningThread(t, time.Now().Add(time.Hour))
		r.fail("holders")
		defer r.heal()
		return errRefused(r.ds.settleInterruptedThreads(context.Background()))
	},
	"agents.go:settleLostThreads": func(t *testing.T, r *runsRepository) (bool, string) {
		r.runningThread(t, time.Now().Add(-time.Hour))
		r.fail("holders")
		defer r.heal()
		return errRefused(r.ds.settleLostThreads(context.Background()))
	},
	"approvals.go:runApproval": func(t *testing.T, r *runsRepository) (bool, string) {
		id := r.approvalIn(t, approvalApproved)
		r.fail("holders")
		defer r.heal()
		r.ds.runApproval(context.Background(), id)
		row := approvalRow(t, r.ds, id)
		return row.States[propApprovalState] == approvalApproved && row.Props["reportedAt"] == nil, fmt.Sprint(row.States, row.Props["reportedAt"])
	},
	"approvals.go:settleInterruptedApprovals": func(t *testing.T, r *runsRepository) (bool, string) {
		r.approvalIn(t, approvalRunning)
		r.fail("holders")
		defer r.heal()
		return errRefused(r.ds.settleInterruptedApprovals(context.Background()))
	},
	"webhooks.go:deliveryBehalf": func(t *testing.T, r *runsRepository) (bool, string) {
		r.staged(t, "scribe", fakeTurn{calls: []fakeCall{{"write", writeArgs(t, "put", r.note, "", map[string]any{"name": "x"})}}}, fakeTurn{content: "done."})
		var seq int64
		if err := r.ds.db.QueryRowContext(context.Background(), `SELECT max(seq) FROM changelog WHERE coalesce(on_behalf_of, '') <> ''`).Scan(&seq); err != nil {
			return false, err.Error()
		}
		r.fail("holders")
		defer r.heal()
		_, err := r.ds.deliveryBehalf(context.Background(), seq)
		return errRefused(err)
	},
	"webhookspool.go:drainSpooled": func(t *testing.T, r *runsRepository) (bool, string) {
		ctx := context.Background()
		if _, err := r.ds.Put(ctx, substrate.ActorSystem, substrate.PutInput{Kind: typeTrigger, ID: "hook", Properties: map[string]any{
			"label": "hook", "enabled": true, "source": map[string]any{"webhook": map[string]any{}},
			"callable": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindFunction, r.pkg+"/reader")},
		}}); err != nil {
			return false, err.Error()
		}
		tr, _, err := r.ds.triggerByID(ctx, "hook")
		if err != nil {
			return false, err.Error()
		}
		as := substrate.WithOnBehalfOf(substrate.WithIdentity(ctx, r.adaID), substrate.ActorPerson)
		name, _, err := r.ds.writeSpooledWebhook(as, tr, substrate.WebhookRequest{Method: "POST", ContentType: "application/json", Body: []byte(`{}`)}, nowUTC(), false)
		if err != nil {
			return false, err.Error()
		}
		r.fail("holders")
		defer r.heal()
		_, err = r.ds.drainSpooled(ctx, name)
		return errRefused(err)
	},
}

func pinnedRefused(t *testing.T, r *runsRepository) (bool, string) {
	if _, err := r.ds.Put(r.adaCtx(), substrate.ActorPerson, substrate.PutInput{Kind: typeProvider, ID: "pinned-provider", Properties: map[string]any{
		"wire": "openai", "baseURL": "https://pinned.example.com", "apiKey": "row-key-pinned",
	}}); err != nil {
		return false, err.Error()
	}
	r.fail("roles")
	defer r.heal()
	r.chat(t, r.adaCtx(), substrate.ActorPerson, "pinner", "", fakeTurn{content: "first."})
	reqs := r.fake.requestsOf("pinner")
	return len(reqs) == 1 && !requestCarries(reqs[0], "pinned.example.com"), "the block carried the providers"
}

func hostReadRefused(op string) func(t *testing.T, r *runsRepository) (bool, string) {
	return func(t *testing.T, r *runsRepository) (bool, string) {
		r.fail("roles")
		defer r.heal()
		_, _, err := r.ds.CallFunction(r.adaCtx(), substrate.ActorPerson, r.pkg+"/reader", map[string]any{"op": op})
		return errRefused(err)
	}
}

func personRolesRefused(t *testing.T, r *runsRepository) (bool, string) {
	r.fail("roles")
	defer r.heal()
	return errRefused(r.r.s.SetPersonRoles(context.Background(), r.bo, r.ada, "viewer"))
}

// proposed is a change request ada's proposer made, with the reads healthy.
func (r *runsRepository) proposed(t *testing.T) *substrate.Record {
	t.Helper()
	r.staged(t, "proposer", fakeTurn{calls: []fakeCall{{"propose", toolArgs(t, map[string]any{
		"kind": r.note, "target": fid("n1"), "diff": map[string]any{"properties": map[string]any{"name": "better"}}, "rationale": "tidy",
	})}}}, fakeTurn{content: "proposed."})
	return onlyPatchRequest(t, r.ds)
}

// runningThread is one of ada's threads left `running`, its lease until.
func (r *runsRepository) runningThread(t *testing.T, until time.Time) {
	t.Helper()
	thread := r.staged(t, "scribe", fakeTurn{content: "first."})
	if _, err := r.ds.db.ExecContext(context.Background(), `UPDATE records SET props = props || jsonb_build_object('status', $3::text, 'leaseUntil', $4::text)
		WHERE kind = $1 AND id = $2`, typeThread, thread, threadRunning, until.UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
}

// approvalIn is ada's saver's approval, put in state with the reads healthy.
func (r *runsRepository) approvalIn(t *testing.T, state string) string {
	t.Helper()
	thread := r.staged(t, "saver", fakeTurn{calls: []fakeCall{{"savenote", `{}`}}}, fakeTurn{content: "it waits."})
	held := approvalsOf(t, r.ds, thread)
	if len(held) != 1 {
		t.Fatalf("approvals = %d, want 1", len(held))
	}
	if _, err := r.ds.db.ExecContext(context.Background(), `UPDATE records SET states = jsonb_set(states, '{state}', to_jsonb($3::text)) WHERE kind = $1 AND id = $2`,
		vocabulary.KindLLMApproval, held[0].ID, state); err != nil {
		t.Fatal(err)
	}
	return held[0].ID
}

func TestEveryBoundaryRefusesWhenItsRulesCannotBeRead(t *testing.T) {
	t.Parallel()
	found := ruleBoundarySites(t)
	for _, site := range found {
		if _, ok := ruleBoundaries[site]; !ok {
			t.Errorf("%s reads the rules a check holds a hand to and has no row in ruleBoundaries: add one that drives it with the reads failing", site)
		}
	}
	for site := range ruleBoundaries {
		if !slices.Contains(found, site) {
			t.Errorf("ruleBoundaries has a row for %s, which no longer reads the rules: take it out", site)
		}
	}
	for _, site := range slices.Sorted(func(yield func(string) bool) {
		for k := range ruleBoundaries {
			if !yield(k) {
				return
			}
		}
	}) {
		drive := ruleBoundaries[site]
		t.Run(site, func(t *testing.T) {
			t.Parallel()
			r := newRunsRepository(t)
			r.seededNotes(t)
			if refused, saw := drive(t, r); !refused {
				t.Errorf("%s answered with the rules unread: %s", site, saw)
			}
		})
	}
}

// ruleBoundarySites are the package's functions that call a reader of the
// rules and are none themselves, as file:function.
func ruleBoundarySites(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || slices.Contains(ruleReaders, fn.Name.Name) {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch x := c.Fun.(type) {
				case *ast.SelectorExpr:
					name = x.Sel.Name
				case *ast.Ident:
					name = x.Name
				}
				if slices.Contains(ruleReaders, name) {
					seen[f+":"+fn.Name.Name] = true
				}
				return true
			})
		}
	}
	return slices.Sorted(func(yield func(string) bool) {
		for k := range seen {
			if !yield(k) {
				return
			}
		}
	})
}
