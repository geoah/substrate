package api

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// assistantDataset is the dataset an assistant's `mcp` token reaches at the
// MCP door (decision 0230): the engine's, at the owner's tier and without a
// scope mark, so it applies vocabulary, installs packages, calls functions
// and chats with agents as the owner's own client does. Every method is
// spelled out and nothing is embedded, so a method added to
// substrate.Dataset does not compile here until somebody decides what an
// assistant may do with it.
//
// It reads what the owner reads, the token records among them, whose
// secrets are digests and secret-typed values that read back redacted for
// every token. What it takes away:
//   - writing the auth material: no put, patch, delete or merge of the four
//     vocabulary.AuthKinds, and no accept of a change request whose target
//     is one, which would write it under the owner's hand;
//   - a webhook trigger's key, which is a credential handed out in the clear:
//     it is cut from every trigger read back, from the change feed's trigger
//     entries, and from what a list may filter or order on;
//   - the token mint, the export, a provider account's connect flow, domain
//     verification, an app's set up bookkeeping, a package takeover and the
//     engine's maintenance passes;
//   - a package's uninstall and purge, which delete what a provider synced.
//
// Code the assistant declares runs as the owner's code does and may reach
// what the owner's code may, secrets included; the approval page says so.
type assistantDataset struct {
	inner substrate.Dataset
}

var _ substrate.Dataset = (*assistantDataset)(nil)

func newAssistantDataset(inner substrate.Dataset) *assistantDataset {
	return &assistantDataset{inner: inner}
}

// errAssistant is the refusal of everything an assistant's token does not
// reach.
func errAssistant(what string) error {
	return fmt.Errorf("%w: an assistant's token does not reach %s", substrate.ErrForbidden, what)
}

// refuseAuthWrite refuses a write of a kind holding auth material.
func refuseAuthWrite(kind string) error {
	if vocabulary.AuthKinds[kind] {
		return errAssistant("writing " + kind + ": the owner's auth material")
	}
	return nil
}

// refuseTriggerSource refuses a condition or an order on a trigger's source
// in a list that may reach triggers: the engine compares the stored text,
// webhook key included, and a cursor carries the last row's sort values.
func refuseTriggerSource(f substrate.Filter, order []substrate.Order) error {
	if len(f.Kinds) > 0 && !slices.Contains(f.Kinds, kindTrigger) {
		return nil
	}
	onSource := func(name string) bool {
		return name == triggerSource || strings.HasPrefix(name, triggerSource+".")
	}
	for name := range f.Properties {
		if onSource(name) {
			return errAssistant("a trigger's source in a filter")
		}
	}
	for _, o := range order {
		if onSource(o.Property) {
			return errAssistant("a trigger's source in an order")
		}
	}
	return nil
}

// withoutKey returns rec without a trigger's webhook key; any other record
// is returned as it is.
func withoutKey(rec *substrate.Record) *substrate.Record {
	if rec == nil || rec.Kind != kindTrigger {
		return rec
	}
	cp := *rec
	withoutWebhookKeys(&cp)
	return &cp
}

func withoutKeys(recs []*substrate.Record) []*substrate.Record {
	for i, rec := range recs {
		recs[i] = withoutKey(rec)
	}
	return recs
}

func withoutKeysIncluded(included map[string]*substrate.Record) map[string]*substrate.Record {
	for path, rec := range included {
		included[path] = withoutKey(rec)
	}
	return included
}

// withoutTriggerPayloads drops what a trigger's changes carry of its
// properties, the webhook key among them: the entry still says which trigger
// changed, when and by whom.
func withoutTriggerPayloads(changes []substrate.Change) []substrate.Change {
	for i := range changes {
		c := &changes[i]
		// Which token stood behind a write is the owner's to read, as on an
		// app's grant (scopedDataset.heldRow).
		c.Token = ""
		if c.Kind == kindTrigger {
			c.Payload = nil
		}
		if slices.ContainsFunc(c.Affected, func(a substrate.AffectedRecord) bool { return a.Kind == kindTrigger && a.Properties != nil }) {
			affected := slices.Clone(c.Affected)
			for j := range affected {
				if affected[j].Kind == kindTrigger {
					affected[j].Properties = nil
				}
			}
			c.Affected = affected
		}
	}
	return changes
}

// --- vocabulary and units ---

func (a *assistantDataset) Repository() substrate.RepositoryInfo { return a.inner.Repository() }

func (a *assistantDataset) Kinds(ctx context.Context) ([]substrate.KindInfo, error) {
	return a.inner.Kinds(ctx)
}

func (a *assistantDataset) KindRetentions(ctx context.Context) ([]substrate.KindRetention, error) {
	return a.inner.KindRetentions(ctx)
}

func (a *assistantDataset) KindByRef(ctx context.Context, ref string) (substrate.KindInfo, error) {
	return a.inner.KindByRef(ctx, ref)
}

func (a *assistantDataset) TypesImplementing(ctx context.Context, trait string) ([]substrate.KindInfo, error) {
	return a.inner.TypesImplementing(ctx, trait)
}

func (a *assistantDataset) Units(ctx context.Context) (substrate.UnitTable, error) {
	return a.inner.Units(ctx)
}

func (a *assistantDataset) ConvertUnit(ctx context.Context, in substrate.UnitConvert) (substrate.UnitConversion, error) {
	return a.inner.ConvertUnit(ctx, in)
}

func (a *assistantDataset) ApplyVocabularyDocuments(ctx context.Context, actor substrate.Actor, docs []map[string]any) ([]*substrate.Record, error) {
	recs, err := a.inner.ApplyVocabularyDocuments(ctx, actor, docs)
	return withoutKeys(recs), err
}

func (a *assistantDataset) ApplyVocabularyDocumentsWith(ctx context.Context, actor substrate.Actor, docs []map[string]any, opts substrate.VocabularyApply) ([]*substrate.Record, error) {
	recs, err := a.inner.ApplyVocabularyDocumentsWith(ctx, actor, docs, opts)
	return withoutKeys(recs), err
}

func (a *assistantDataset) PlanVocabularyApply(ctx context.Context, actor substrate.Actor, docs []map[string]any) (substrate.VocabularyPlan, error) {
	return a.inner.PlanVocabularyApply(ctx, actor, docs)
}

func (a *assistantDataset) PlanVocabularyApplyWith(ctx context.Context, actor substrate.Actor, docs []map[string]any, opts substrate.VocabularyApply) (substrate.VocabularyPlan, error) {
	return a.inner.PlanVocabularyApplyWith(ctx, actor, docs, opts)
}

func (a *assistantDataset) PlanShippedUpgrade(ctx context.Context) ([]substrate.ShippedUpgrade, error) {
	return a.inner.PlanShippedUpgrade(ctx)
}

// TakeOverPackage moves a package an app keeps into the owner's hands
// (decision 0205): a decision about another app's access, which is the
// owner's.
func (a *assistantDataset) TakeOverPackage(context.Context, substrate.Actor, string, string) (*substrate.Record, error) {
	return nil, errAssistant("a package takeover")
}

// --- records ---

func (a *assistantDataset) Get(ctx context.Context, typ, id string) (*substrate.Record, error) {
	rec, err := a.inner.Get(ctx, typ, id)
	return withoutKey(rec), err
}

func (a *assistantDataset) List(ctx context.Context, q substrate.Query) (*substrate.Page, error) {
	if err := refuseTriggerSource(q.Filter, q.OrderBy); err != nil {
		return nil, err
	}
	page, err := a.inner.List(ctx, q)
	if err != nil || page == nil {
		return page, err
	}
	page.Records = withoutKeys(page.Records)
	page.Included = withoutKeysIncluded(page.Included)
	return page, nil
}

func (a *assistantDataset) Search(ctx context.Context, in substrate.SearchInput) (substrate.SearchResult, error) {
	res, err := a.inner.Search(ctx, in)
	for i := range res.Hits {
		res.Hits[i].Record = withoutKey(res.Hits[i].Record)
	}
	return res, err
}

func (a *assistantDataset) Window(ctx context.Context, q substrate.WindowQuery) (*substrate.WindowPage, error) {
	if err := refuseTriggerSource(q.Filter, nil); err != nil {
		return nil, err
	}
	page, err := a.inner.Window(ctx, q)
	if err != nil || page == nil {
		return page, err
	}
	page.Rows = withoutKeys(page.Rows)
	page.Series = withoutKeys(page.Series)
	page.Overrides = withoutKeys(page.Overrides)
	page.Included = withoutKeysIncluded(page.Included)
	return page, nil
}

func (a *assistantDataset) Aggregate(ctx context.Context, q substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	return a.inner.Aggregate(ctx, q)
}

func (a *assistantDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(in.Kind); err != nil {
		return nil, err
	}
	rec, err := a.inner.Put(ctx, actor, in)
	return withoutKey(rec), err
}

func (a *assistantDataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(typ); err != nil {
		return nil, err
	}
	if typ == vocabulary.KindRecordPatchRequest {
		if _, deciding := in.Properties["decision"]; deciding {
			if err := a.refuseAuthTarget(ctx, id); err != nil {
				return nil, err
			}
		}
	}
	rec, err := a.inner.Patch(ctx, actor, typ, id, in)
	return withoutKey(rec), err
}

// refuseAuthTarget refuses a decision on a change request whose accept would
// create, patch or delete auth material: the accept writes its target under
// the owner's hand, past every check on the request itself.
func (a *assistantDataset) refuseAuthTarget(ctx context.Context, id string) error {
	req, err := a.inner.Get(ctx, vocabulary.KindRecordPatchRequest, id)
	if err != nil || req == nil {
		// The patch itself answers a missing request the way it always does.
		return nil
	}
	if k, _ := req.Properties["targetKind"].(string); k != "" {
		if err := refuseAuthWrite(k); err != nil {
			return err
		}
	}
	for _, target := range referenceValues(req.Properties["target"]) {
		if k, _, ok := vocabulary.SplitRecordPath(target); ok {
			if err := refuseAuthWrite(k); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *assistantDataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(typ); err != nil {
		return nil, err
	}
	rec, err := a.inner.Delete(ctx, actor, typ, id, in)
	return withoutKey(rec), err
}

func (a *assistantDataset) Pin(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(typ); err != nil {
		return nil, err
	}
	rec, err := a.inner.Pin(ctx, actor, typ, id, in)
	return withoutKey(rec), err
}

func (a *assistantDataset) Release(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.MarkInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(typ); err != nil {
		return nil, err
	}
	rec, err := a.inner.Release(ctx, actor, typ, id, in)
	return withoutKey(rec), err
}

// Forget is the owner's alone, which the engine holds the assistant's hand to.
func (a *assistantDataset) Forget(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.ForgetInput) (*substrate.Erasure, error) {
	if err := refuseAuthWrite(typ); err != nil {
		return nil, err
	}
	return a.inner.Forget(ctx, actor, typ, id, in)
}

func (a *assistantDataset) Merge(ctx context.Context, actor substrate.Actor, in substrate.MergeInput) (*substrate.Record, error) {
	if err := refuseAuthWrite(in.Kind); err != nil {
		return nil, err
	}
	rec, err := a.inner.Merge(ctx, actor, in)
	return withoutKey(rec), err
}

func (a *assistantDataset) Split(ctx context.Context, actor substrate.Actor, in substrate.SplitInput) (*substrate.Record, error) {
	rec, err := a.inner.Split(ctx, actor, in)
	return withoutKey(rec), err
}

// --- the change feed ---

func (a *assistantDataset) Changes(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	changes, err := a.inner.Changes(ctx, after, f, limit)
	return withoutTriggerPayloads(changes), err
}

func (a *assistantDataset) ChangesBefore(ctx context.Context, before int64, f substrate.ChangeFilter, limit int) ([]substrate.Change, error) {
	changes, err := a.inner.ChangesBefore(ctx, before, f, limit)
	return withoutTriggerPayloads(changes), err
}

func (a *assistantDataset) ChangeTriggers(ctx context.Context, changes []substrate.Change) (map[int64][]substrate.ChangeTrigger, error) {
	return a.inner.ChangeTriggers(ctx, changes)
}

func (a *assistantDataset) Head(ctx context.Context) (substrate.ChangelogHead, error) {
	return a.inner.Head(ctx)
}

func (a *assistantDataset) WatchSignal(ctx context.Context) <-chan int64 {
	return a.inner.WatchSignal(ctx)
}

// --- the owner's credentials and housekeeping ---

func (a *assistantDataset) MintToken(context.Context, substrate.MintTokenInput) (substrate.TokenInfo, string, error) {
	return substrate.TokenInfo{}, "", errAssistant("minting a token")
}

func (a *assistantDataset) Tokens(ctx context.Context) ([]substrate.TokenInfo, error) {
	return a.inner.Tokens(ctx)
}

func (a *assistantDataset) Export(context.Context) (substrate.Export, error) {
	return nil, errAssistant("the export")
}

func (a *assistantDataset) RunGC(context.Context) (int, error) {
	return 0, errAssistant("the collector")
}

func (a *assistantDataset) ProcessEmbedQueue(context.Context, int) (int, error) {
	return 0, errAssistant("the embed queue")
}

func (a *assistantDataset) Reembed(context.Context, bool) (substrate.ReembedReport, error) {
	return substrate.ReembedReport{}, errAssistant("re-embedding")
}

func (a *assistantDataset) ProcessTriggers(context.Context) (int, error) {
	return 0, errAssistant("the trigger pass")
}

func (a *assistantDataset) SweepResolutions(context.Context) (int, error) {
	return 0, errAssistant("the resolution sweep")
}

func (a *assistantDataset) RefreshOAuthTokens(context.Context) (int, error) {
	return 0, errAssistant("provider token refresh")
}

func (a *assistantDataset) ProcessOAuthFinalizers(context.Context) (int, error) {
	return 0, errAssistant("provider token finalizers")
}

// StartOAuth begins a provider account's connect flow, whose consent URL
// carries the flow's credential and is the owner's to open in their browser.
func (a *assistantDataset) StartOAuth(context.Context, substrate.Actor, string) (string, error) {
	return "", errAssistant("a provider account's connect flow: the owner connects it in the console")
}

func (a *assistantDataset) DomainState(ctx context.Context) (substrate.DomainState, error) {
	return a.inner.DomainState(ctx)
}

func (a *assistantDataset) StartDomainVerification(context.Context) (substrate.DomainState, error) {
	return substrate.DomainState{}, errAssistant("domain verification")
}

func (a *assistantDataset) MarkDomainVerified(context.Context, string) (time.Time, error) {
	return time.Time{}, errAssistant("domain verification")
}

func (a *assistantDataset) RecordAppBuild(context.Context, map[string]int64, string) error {
	return errAssistant("an app's set up")
}

func (a *assistantDataset) CompleteAppAddon(context.Context, string, string) error {
	return errAssistant("an app's set up")
}

// --- triggers ---

func (a *assistantDataset) TriggerStatuses(ctx context.Context) ([]substrate.TriggerStatus, error) {
	return a.inner.TriggerStatuses(ctx)
}

func (a *assistantDataset) SyncStatuses(ctx context.Context) ([]substrate.SyncStatus, error) {
	return a.inner.SyncStatuses(ctx)
}

func (a *assistantDataset) ReplayTrigger(ctx context.Context, id string, from int64) error {
	return a.inner.ReplayTrigger(ctx, id, from)
}

func (a *assistantDataset) RunTrigger(ctx context.Context, id, recordKind, recordID string) (int, error) {
	return a.inner.RunTrigger(ctx, id, recordKind, recordID)
}

func (a *assistantDataset) WakeTrigger(ctx context.Context, id string) (int, error) {
	return a.inner.WakeTrigger(ctx, id)
}

func (a *assistantDataset) TriggerFailures(ctx context.Context, id string) ([]substrate.TriggerFailure, error) {
	return a.inner.TriggerFailures(ctx, id)
}

func (a *assistantDataset) RetryTriggerFailure(ctx context.Context, id string, failureID int64) (int, error) {
	return a.inner.RetryTriggerFailure(ctx, id, failureID)
}

func (a *assistantDataset) ForgetTriggerFailure(ctx context.Context, id string, failureID int64) error {
	return a.inner.ForgetTriggerFailure(ctx, id, failureID)
}

// --- functions and agents ---

func (a *assistantDataset) CallFunction(ctx context.Context, caller substrate.Actor, name string, args any) (any, int, error) {
	return a.inner.CallFunction(ctx, caller, name, args)
}

func (a *assistantDataset) CallAgent(ctx context.Context, name string, input any) (*substrate.AgentResult, error) {
	return a.inner.CallAgent(ctx, name, input)
}

func (a *assistantDataset) ChatAgent(ctx context.Context, actor substrate.Actor, name string, in substrate.ChatInput, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return a.inner.ChatAgent(ctx, actor, name, in, emit)
}

func (a *assistantDataset) AttachAgentTurn(ctx context.Context, threadID string, emit func(substrate.AgentEvent)) (*substrate.AgentResult, error) {
	return a.inner.AttachAgentTurn(ctx, threadID, emit)
}

func (a *assistantDataset) StopAgentTurn(ctx context.Context, actor substrate.Actor, threadID string) (*substrate.AgentResult, error) {
	return a.inner.StopAgentTurn(ctx, actor, threadID)
}

// --- packages ---

func (a *assistantDataset) BundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	return a.inner.BundleStatuses(ctx)
}

func (a *assistantDataset) BundleStatusesUncounted(ctx context.Context) ([]substrate.BundleStatus, error) {
	return a.inner.BundleStatusesUncounted(ctx)
}

func (a *assistantDataset) BundleStatus(ctx context.Context, id string) (substrate.BundleStatus, error) {
	return a.inner.BundleStatus(ctx, id)
}

func (a *assistantDataset) BundlePackage(ctx context.Context, id string) (string, error) {
	return a.inner.BundlePackage(ctx, id)
}

func (a *assistantDataset) DisableBundle(ctx context.Context, id string) error {
	return a.inner.DisableBundle(ctx, id)
}

func (a *assistantDataset) EnableBundle(ctx context.Context, id string) error {
	return a.inner.EnableBundle(ctx, id)
}

func (a *assistantDataset) BindBundleInput(ctx context.Context, id, input, record string) error {
	return a.inner.BindBundleInput(ctx, id, input, record)
}

// UninstallBundle and PurgeBundle delete what a provider synced, the owner's
// mail and calendar among it: the owner decided an assistant never does
// either (decision 0230).
func (a *assistantDataset) UninstallBundle(context.Context, string) error {
	return errAssistant("uninstalling a package: the owner does it in the console")
}

func (a *assistantDataset) PurgeBundle(context.Context, string) (int, error) {
	return 0, errAssistant("purging a package: the owner does it in the console")
}

func (a *assistantDataset) InstallBundleClosure(ctx context.Context, actor substrate.Actor, vocabularyDocs []map[string]any, dataDocs []substrate.PutInput, opts substrate.BundleInstall) ([]*substrate.Record, error) {
	recs, err := a.inner.InstallBundleClosure(ctx, actor, vocabularyDocs, dataDocs, opts)
	return withoutKeys(recs), err
}

func (a *assistantDataset) PlanBundleUpgrade(ctx context.Context, vocabularyDocs []map[string]any) (substrate.BundleUpgrade, error) {
	return a.inner.PlanBundleUpgrade(ctx, vocabularyDocs)
}

// --- blobs ---

func (a *assistantDataset) PutBlob(ctx context.Context, actor substrate.Actor, up substrate.BlobUpload, data []byte, wantDigest string) (*substrate.BlobInfo, error) {
	return a.inner.PutBlob(ctx, actor, up, data, wantDigest)
}

func (a *assistantDataset) GetBlob(ctx context.Context, digest string) (*substrate.BlobInfo, []byte, error) {
	return a.inner.GetBlob(ctx, digest)
}

func (a *assistantDataset) StatBlob(ctx context.Context, digest string) (*substrate.BlobInfo, error) {
	return a.inner.StatBlob(ctx, digest)
}

// --- operator defaults ---

// OperatorDefaults is a read of what the operator pays for and today's
// allowance; it carries no key, so the assistant reads it as the owner does.
func (a *assistantDataset) OperatorDefaults(ctx context.Context) (substrate.OperatorDefaults, error) {
	return a.inner.OperatorDefaults(ctx)
}

// ModelOffers names model ids per row and carries no key, so the assistant
// reads it as the owner does.
func (a *assistantDataset) ModelOffers(ctx context.Context) (map[string][]string, error) {
	return a.inner.ModelOffers(ctx)
}
