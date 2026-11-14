package engine

// EVERY PLACE THE ENGINE TAKES THE SYSTEM'S HAND, LISTED (decision 0290). The
// system writes any property, so a site that writes as it must be one where
// the engine itself decides the value or replays one, or one that carries
// another hand's value and says what that value is held to. Each function of
// this package that names a hand of the system's type, substrate.ActorSystem,
// the OAuth facility's actorOAuth, the webhook door's actorWebhook or a
// change-request policy's `policy:` actor, is listed here with which:
//
//   - decides: the engine's own value (its bookkeeping, a replay, a
//     migration's rewrite);
//   - carries: a value another hand chose, and the check it is held to;
//   - names: the system named without writing as it (an attribution, a
//     comparison).
//
// A new site fails the test until it is listed, and a site that goes fails it
// until its line does.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var systemHands = map[string]string{
	"actors.go:ensureLinks":                   "decides: the actor records of a write's hands, the system's first",
	"actors.go:ensureActor":                   "decides: one actor record the engine keeps",
	"actors.go:disableActor":                  "decides: an app's actor record disabled when its last token goes",
	"actorfill.go:fillActorRecords":           "decides: the actor records a repository links and holds none of, written at an open",
	"addonconsent.go:CompleteAppAddon":        "carries: the person's approval of an app's add-on onto the app's token record; held to the person's own unscoped hand, into a core kind that declares no writer:",
	"agentjobs.go:settleInterruptedJobs":      "decides: an agent job cut short by a stop",
	"agents.go:claimAgentDelivery":            "decides: an agent delivery's claim",
	"agents.go:settleInterruptedClaims":       "decides: claims a stop left in flight",
	"agents.go:markRerun":                     "decides: a failed agent delivery marked to run again",
	"agents.go:settleLostThreads":             "decides: threads whose run was lost",
	"agents.go:settleInterruptedThreads":      "decides: threads a stop cut short",
	"alerts.go:alertActorOf":                  "names: an alert with no callable behind it is the system's",
	"alerts.go:alertActorOfTrigger":           "names: the same, for a trigger's alerts",
	"alerts.go:alertActorOfAbout":             "names: the same, for what an alert is about",
	"appbuild.go:RecordAppBuild":              "carries: an app's reported build and package floors onto its own token record; held to the app's own live token, into a core kind that declares no writer:",
	"auth.go:MintToken":                       "carries: the minter's label, scopes and app facts onto a new token; held to the mint's own input checks, into a core kind that declares no writer:",
	"auth.go:ResetUser":                       "carries: the person an operator's reset gives a repository, as an actor naming the identity the reset made and the name the operator chose (or the actor's, or the repository's); held to the operator's own door on the box, which nothing on the network reaches, validIdentityName and the identity's insert in the reset's control-plane transaction, into a core kind that declares no writer:",
	"blobs.go:blobGCPass":                     "decides: unreferenced blobs collected",
	"bundles.go:DisableBundle":                "decides: the disabled mark a person's request sets",
	"bundles.go:EnableBundle":                 "decides: the disabled mark a person's request clears",
	"bundles.go:UninstallBundle":              "decides: a package's declarations removed",
	"bundles.go:PurgeBundle":                  "decides: the purging mark and the purge a person's request runs",
	"bundles.go:purgeTypes":                   "decides: a purge's record deletions",
	"callrun.go:putFailedCallRun":             "decides: the run record of a call that failed",
	"dataset.go:metaKeyAllowed":               "names: the system may write any label or annotation key",
	"delivery.go:settleDelivery":              "decides: a delivery's ledger entry",
	"domain.go:StartDomainVerification":       "decides: a domain proof to publish",
	"domain.go:MarkDomainVerified":            "decides: a domain proven",
	"domain.go:reconcileRepositoryName":       "decides: the repository's own record and its first person's actor take the name its row has, after an import under another name",
	"localtitles.go:renderLocalTitles":        "decides: the mark that the titles under ~ are rendered, once, on the repository's own record",
	"localtitles.go:renderTitlesSpelling":     "decides: a title a template rendered from a name the local namespace's conversion moved, rendered again by the engine's renderer",
	"engine.go:createSeededRepositoryFor":     "carries: the registration's own records beside the seed, the system and person actors (the person's naming the identity the registration made) and the default providers it decides; held to the registration door's checks, into core kinds that declare no writer:",
	"functions.go:parkAndAdvance":             "decides: a failed delivery parked and its cursor moved",
	"functions.go:recordSkipAndAdvance":       "decides: a skipped run and its cursor",
	"functions.go:skipClaimed":                "decides: a claimed delivery skipped",
	"functions.go:deliverFire":                "decides: a schedule fire's state",
	"functions.go:ensureScheduleState":        "decides: a schedule's fire state",
	"functions.go:putSystemRun":               "decides: a run record",
	"functions.go:sweepPagedCursors":          "decides: paged chains a stop cut short",
	"functions.go:ensureCursor":               "decides: a trigger's cursor",
	"functions.go:ReplayTrigger":              "decides: a trigger's cursor rewound, at a person's request",
	"functions.go:retryHeldFailure":           "decides: a held failure's settlement",
	"functions.go:ForgetTriggerFailure":       "decides: a failure dropped, at a person's request",
	"gc.go:sweepUnheldSealed":                 "decides: sealed values no record holds, erased",
	"gc.go:gcPass":                            "decides: deleted records collected",
	"gc.go:cascadeOwned":                      "decides: records a deleted owner held",
	"health.go:raiseFailingAlerts":            "decides: a failing trigger's alert",
	"health.go:resolveStaleFailing":           "decides: a stale alert resolved",
	"health.go:resolveGoneTriggerAlerts":      "decides: the alerts of a trigger that is gone",
	"inputs.go:BindBundleInput":               "carries: the person's choice of the record that fills a package's input onto the bundle row; held to the owner's door and the record's live kind, into a core kind that declares no writer:",
	"judge.go:judgeRequest":                   "carries: the policy judge's verdict as an annotation on a change request; held to an annotation being no property",
	"judge.go:decideAsPolicy":                 "carries: a request's diff that a person's policy rule accepts; held to the person's type, as a person's own acceptance is (writerOf)",
	"oauth.go:(package)":                      "names: the OAuth facility's hand",
	"people.go:AddPerson":                     "carries: the person an operator adds to a repository, as an actor naming the identity the operator named; held to that identity being one this server holds (identityByName) and to the operator's own door, which nothing on the network reaches, into a core kind that declares no writer:",
	"people.go:namePersonActor":               "carries: the identity and the name its caller gives onto a person's actor; held to its callers' checks (each listed) and to never writing again an actor the history disabled or deleted, into a core kind that declares no writer:",
	"people.go:nameTokensPeople":              "decides: the person of every token from before identities, the repository's first person",
	"people.go:SetPersonRoles":                "carries: the roles an operator gives a person; held to the operator's own door on the box, which nothing on the network reaches, and to setPersonRoles' checks",
	"people.go:setPersonRoles":                "carries: the roles its caller gives a person's actor; held to each being a role the repository holds and to never leaving the repository without an admin, its callers' checks (each listed) beside them, into a core kind that declares no writer:",
	"people.go:settlePeople":                  "decides: the identity the boot gave a repository from before identities, written onto its first person's actor",
	"oauth.go:noteTokenRefresh":               "decides: the OAuth facility's token state after a refresh",
	"oauth.go:completeOAuth":                  "decides: the OAuth facility's slots of an account it connected",
	"operatoroauth.go:settleOAuthClientStamp": "decides: the OAuth client an account's grant names",
	"webhooks.go:(package)":                   "names: the webhook door's hand",
	"webhooks.go:admitWebhook":                "carries: a sender's request bytes into blob records; held to the trigger's declared webhook and its verification, into a core kind that declares no writer:",
	"webhooks.go:recordWebhook":               "carries: a sender's request as a pending delivery; held to the trigger's declared webhook, into the delivery ledger and core kinds that declare no writer:",
	"webhooks.go:stage":                       "carries: one part of a sender's request as a blob; held to the trigger's declared webhook, into a core kind that declares no writer:",
	"write.go:isPolicyActor":                  "names: a change-request policy's hand, which writerOf holds as the person",
	"lostsecrets.go:clearSecretsLeftBehind":   "decides: the secret refs a restore without the data key left behind, cleared from the records that held them",
	"lostsecrets.go:clearLeftBehind":          "decides: one ref taken out of the record that held it, and the alert that says so",
	"lostsecrets.go:settleSecretsLeftBehind":  "decides: the alert of a secret its owner gave a value again, resolved",
	"mapping.go:asRecompute":                  "carries: recompute's writes, the winning source's values into the mapped target, their removal where no source offers them and the marks it moves; held to no guarded property: a recompute writes none, whoever started it (checkPropertyOwnership)",
	"oauth.go:ProcessOAuthFinalizers":         "decides: the OAuth finalizer lifted after a revoke",
	"orphans.go:collectOrphan":                "decides: an orphan collected",
	"roles.go:ensureShippedRoles":             "decides: the shipped role records, admin, editor and viewer, as this binary defines them",
	"rebuild.go:RebuildRepository":            "decides: the changelog replayed",
	"repodir.go:refold":                       "decides: the table's history replayed into the fold",
	"repomigrate.go:runRepositoryMigrations":  "decides: a repository migration's rewrite",
	"repomigration_0002_qualify_bare_selector_kinds.go:migrateQualifyBareSelectorKinds":                   "decides: a migration's rewrite",
	"repomigration_0003_name_units_by_unit_records.go:migrateRow":                                         "decides: a migration's rewrite",
	"repomigration_0003_name_units_by_unit_records.go:unitFor":                                            "decides: a migration's unit record",
	"repomigration_0004_name_applet_runtimes.go:migrateNameAppletRuntimes":                                "decides: a migration's rewrite",
	"repomigration_0005_remove_reply_relay_contacts.go:detachRelayEntries":                                "decides: a migration's rewrite",
	"repomigration_0005_remove_reply_relay_contacts.go:migrateTombstone":                                  "decides: a migration's tombstone",
	"repomigration_0006_key_the_repository_record_by_its_id.go:migrateKeyTheRepositoryRecordByItsID":      "decides: a migration's rewrite",
	"repomigration_0007_start_paged_drains_over.go:migrateStartPagedDrainsOver":                           "decides: a migration's mark on the repository record",
	"repomigration_0008_purge_the_recovery_key.go:migratePurgeTheRecoveryKey":                             "decides: a migration's purge of the recovery record",
	"repomigration_0009_link_actors.go:writeHistoryActors":                                                "names: the system's actor is live in every repository",
	"repomigration_0009_link_actors.go:repositoryHands":                                                   "names: the system is one of every repository's hands",
	"repomigration_0011_drop_a_body_its_kind_does_not_declare.go:dropBodies":                              "decides: a migration's removal of a body no declaration serves",
	"repomigration_0012_move_the_sign_in_out_of_the_repository.go:migrateMoveTheSignInOutOfTheRepository": "decides: a migration's purge of the sign-in record",
	"seed.go:isSubstratePath":                 "names: the system's writes are the substrate's own path",
	"seed.go:upgradeShippedVocabulary":        "decides: the shipped upgrade",
	"seedproviders.go:seedDefaultProviders":   "names: whether a hand other than the system's wrote a default provider",
	"seedproviders.go:ensureDefaultProviders": "decides: the default provider rows",
	"settings.go:purgeSettings":               "decides: a purged package's settings",
	"signout.go:endTokens":                    "decides: every token ended, at a person's request",
	"sync.go:settleInterruptedSyncs":          "decides: a sync's run cut short by a stop",
	"temporaryretry.go:setAsideWaiting":       "decides: a waiting retry set aside",
	"vocabularywrite.go:shippedHands":         "names: the hands a shipped record is the shipper's under",
	"vocabularywrite.go:markGroupQuarantined": "decides: a package's quarantine mark",
	"vocabularywrite.go:clearGroupQuarantine": "decides: a package's quarantine mark lifted",
}

func TestEveryPlaceTheEngineTakesTheSystemsHandIsListed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, decl := range file.Decls {
			where := f + ":(package)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				where = f + ":" + fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "substrate" && x.Sel.Name == "ActorSystem" {
						found[where] = true
					}
					return false
				case *ast.Ident:
					if x.Name == "actorOAuth" || x.Name == "actorWebhook" {
						found[where] = true
					}
				case *ast.BasicLit:
					if strings.HasPrefix(x.Value, `"policy:`) {
						found[where] = true
					}
				}
				return true
			})
		}
	}
	for _, site := range slices.Sorted(func(yield func(string) bool) {
		for k := range found {
			if !yield(k) {
				return
			}
		}
	}) {
		if _, ok := systemHands[site]; !ok {
			t.Errorf("%s takes or names the system's hand and is not in systemHands: list it with decides, carries (and what it is held to) or names", site)
		}
	}
	for site, why := range systemHands {
		if !found[site] {
			t.Errorf("systemHands lists %s, which no longer names the system: take its line out", site)
		}
		word, _, _ := strings.Cut(why, ":")
		switch {
		case word != "decides" && word != "carries" && word != "names":
			t.Errorf("systemHands says %q of %s: each line starts with decides, carries or names", why, site)
		case word == "carries" && !strings.Contains(why, "held to"):
			t.Errorf("systemHands says %s carries a value without saying what it is held to", site)
		}
	}
}
