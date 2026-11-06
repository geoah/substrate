package engine

// The host's spend ledger and its gate (decision 0230). Every call the host
// pays for, an LLM completion on a host-backed row or an embeddings request on
// the host's embeddings entry, first RESERVES a conservative ceiling in
// host_spend_reservations, in one transaction that sums the repository's
// reservations for the UTC calendar day under an advisory lock, and is
// refused when the sum would pass the host file's budget.centsPerDay. The call
// then SETTLES to its reported cost, failed or not. A call that reported no
// usage, or only part of it, keeps its ceiling unless it reported nothing and
// failed in a way the endpoint cannot have billed (egress.ErrUnprocessed),
// and a reservation still open an hour after it opened (a crash, a lost
// settle) is settled at its ceiling, so the ledger only ever overcounts. The
// ceiling covers one billable attempt, so a host-paid client never retries
// an attempt that may have been billed (llm.Config.RetryOnlyUnbilled).
//
// The gate sits beneath every caller because it wraps the clients
// themselves: hostLLMClient is the llm.Client every agent loop on a
// host-backed row calls, so its turns, its retries, its compaction and its
// thread title all reserve; embedProvider.Embed reserves for the drain, the
// semantic query and the endpoint check alike. A scoped app token's chat and
// search reach the same clients. Before a run opens, hostHold says whether the
// day is already spent, so a trigger is HELD as decision 0149 holds one at
// the person's own cap and a direct call is refused before a thread opens.
//
// The person's own spend cap (spend.go) is a separate thing and reads the
// threads' costUSD; this ledger reads nothing the person can write.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/egress"
	"github.com/geoah/substrate/internal/hostdefaults"
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// hostReservationExpiry is how long a reservation stays open before it
	// is settled at its ceiling.
	hostReservationExpiry = time.Hour
	// hostLedgerKeep is how long a row stays in the ledger: the day it was
	// opened in, and a day past it for the host to read.
	hostLedgerKeep = 48 * time.Hour
	// hostSettleAllowance bounds a settle's own write, which runs on a
	// context the call's cancellation does not reach.
	hostSettleAllowance = 10 * time.Second
	// hostBudgetEpsilon absorbs a float sum's last bits.
	hostBudgetEpsilon = 1e-9
)

// hostDay is the UTC calendar day now falls in, and when it ends.
func hostDay(now time.Time) (start, end time.Time) {
	start = now.UTC().Truncate(24 * time.Hour)
	return start, start.Add(24 * time.Hour)
}

// hostReservation is one open reservation.
type hostReservation struct {
	id      int64
	ceiling float64
}

// hostSpendRefusal is the error a call past the host's allowance answers.
func hostSpendRefusal(used, limit float64, resets time.Time) error {
	return fmt.Errorf("%w: %s", substrate.ErrHostBudget, hostSpendDetail(used, limit, resets))
}

// hostSpendDetail says how much of the day's allowance is gone and when it
// comes back, in whole cents rounded up.
func hostSpendDetail(used, limit float64, resets time.Time) string {
	return fmt.Sprintf("this repository has used %d of the host's %d cents for today, counting calls still running at their ceiling; it resets at %s",
		int64(math.Ceil(used-hostBudgetEpsilon)), int64(limit), resets.UTC().Format(time.RFC3339))
}

// reserveHost reserves ceiling cents for one host-paid call of service, or
// refuses with ErrHostBudget. Reservations of one repository serialize on an
// advisory lock, so two calls cannot both find room for one.
func (ds *dataset) reserveHost(ctx context.Context, service string, ceiling float64) (*hostReservation, error) {
	host := ds.svc.host
	limit := float64(host.Budget.CentsPerDay)
	now := ds.svc.hostNow().UTC()
	dayStart, dayEnd := hostDay(now)
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve host spend: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("hostspend")); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve host spend: %w", err)
	}
	if err := expireHostReservations(ctx, tx, now); err != nil {
		return nil, err
	}
	used, err := hostSpendSince(ctx, tx, dayStart)
	if err != nil {
		return nil, err
	}
	if used+ceiling > limit+hostBudgetEpsilon {
		return nil, hostSpendRefusal(used, limit, dayEnd)
	}
	r := &hostReservation{ceiling: ceiling}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO host_spend_reservations (service, opened_at, ceiling_cents)
		VALUES ($1, $2, $3::float8) RETURNING id`, service, now, ceiling).Scan(&r.id); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve host spend: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve host spend: %w", err)
	}
	return r, nil
}

// expireHostReservations settles every reservation open past
// hostReservationExpiry at its ceiling, and drops rows older than
// hostLedgerKeep.
func expireHostReservations(ctx context.Context, q dbx, now time.Time) error {
	if _, err := q.ExecContext(ctx, `
		UPDATE host_spend_reservations SET settled_cents = ceiling_cents, settled_at = $1
		 WHERE settled_at IS NULL AND opened_at <= $2`, now, now.Add(-hostReservationExpiry)); err != nil {
		return fmt.Errorf("substrate/engine: expire host reservations: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM host_spend_reservations WHERE opened_at < $1`, now.Add(-hostLedgerKeep)); err != nil {
		return fmt.Errorf("substrate/engine: prune host reservations: %w", err)
	}
	return nil
}

// hostSpendSince sums the reservations opened at or after since: each at its
// settled cost, or at its ceiling while it is open.
func hostSpendSince(ctx context.Context, q dbx, since time.Time) (float64, error) {
	var used float64
	if err := q.QueryRowContext(ctx, `
		SELECT coalesce(sum(coalesce(settled_cents, ceiling_cents)), 0)::float8
		  FROM host_spend_reservations WHERE opened_at >= $1`, since).Scan(&used); err != nil {
		return 0, fmt.Errorf("substrate/engine: read host spend: %w", err)
	}
	return used, nil
}

// settleHost records a reservation's cost. A reservation already settled (it
// expired first) keeps what it holds, so a late settle never lowers a ceiling
// the expiry charged. It runs on a context the call's own cancellation does
// not reach: a stopped turn still settles.
func (ds *dataset) settleHost(ctx context.Context, r *hostReservation, cents float64) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostSettleAllowance)
	defer cancel()
	if cents < 0 || math.IsNaN(cents) || math.IsInf(cents, 0) {
		cents = r.ceiling
	}
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE host_spend_reservations SET settled_cents = $2::float8, settled_at = $3
		 WHERE id = $1 AND settled_at IS NULL`, r.id, cents, ds.svc.hostNow().UTC()); err != nil {
		// The row stays open and the expiry settles it at its ceiling.
		ds.svc.log.Warn("substrate: a host-paid call's settle was not written; its ceiling stands",
			"repository", ds.Repository().ID, "reservation", r.id, "error", err)
	}
}

// settleCall settles r after the call it reserved for. cents is what the
// usage the endpoint reported costs, and whole says it reported all of it.
// The call settles to cents when whole, whether or not it then failed (a
// reply with no choices, a vector of the wrong width); to nothing when it
// reported nothing and failed in a way the endpoint cannot have billed
// (egress.ErrUnprocessed: no request left, or every one was refused with a
// redirect or a 4xx); and otherwise at its ceiling, or at cents where a
// partial report already passes the ceiling. A dropped answer, a timeout, a
// 5xx, a reply that does not decode and a success with no usage or part of
// it all keep the ceiling: the endpoint may have done the work.
func (ds *dataset) settleCall(ctx context.Context, r *hostReservation, callErr error, cents float64, whole bool) {
	switch {
	case whole:
		ds.settleHost(ctx, r, cents)
	case cents == 0 && errors.Is(callErr, egress.ErrUnprocessed):
		ds.settleHost(ctx, r, 0)
	default:
		ds.settleHost(ctx, r, max(cents, r.ceiling))
	}
}

// hostSpendToday is the day's spend in cents, split by service.
func (ds *dataset) hostSpendToday(ctx context.Context) (used float64, byService map[string]float64, resets time.Time, err error) {
	start, end := hostDay(ds.svc.hostNow())
	rows, err := ds.db.QueryContext(ctx, `
		SELECT service, coalesce(sum(coalesce(settled_cents, ceiling_cents)), 0)::float8
		  FROM host_spend_reservations WHERE opened_at >= $1 GROUP BY service ORDER BY service`, start)
	if err != nil {
		return 0, nil, end, fmt.Errorf("substrate/engine: read host spend: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byService = map[string]float64{}
	for rows.Next() {
		var service string
		var cents float64
		if err := rows.Scan(&service, &cents); err != nil {
			return 0, nil, end, err
		}
		byService[service] = cents
		used += cents
	}
	return used, byService, end, rows.Err()
}

// --- the LLM gate -----------------------------------------------------------

// Prompt estimation for a reservation. The ceiling must not be beaten by the
// call it covers, so the estimate is generous: a token per three bytes, a
// quarter more on top, and a fixed allowance for the wire's own framing.
const (
	hostPromptBytesPerToken = 3
	hostPromptMargin        = 1.25
	hostPromptFraming       = 256
)

// hostLLMClient is the client every call on a host-backed row goes through:
// it holds the request to the host's model list and output ceiling, reserves
// before the call and settles after.
type hostLLMClient struct {
	ds    *dataset
	entry *hostdefaults.LLM
	price map[string]modelPrice
	inner llm.Client
}

func newHostLLMClient(ds *dataset, pc *providerConfig, inner llm.Client) llm.Client {
	return &hostLLMClient{ds: ds, entry: pc.host, price: pc.pricing, inner: inner}
}

func (c *hostLLMClient) Complete(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Result, error) {
	if _, ok := c.entry.Model(req.Model); !ok {
		return nil, fmt.Errorf("%w: llm/provider row %q runs on the host's key, which does not serve model %q", substrate.ErrValidation, c.entry.ID, req.Model)
	}
	req.Params.MaxTokens = hostMaxTokens(req.Params.MaxTokens, c.entry.MaxOutputTokens)
	ttl := ""
	if req.Cache != nil {
		ttl = req.Cache.TTL
	}
	price := c.price[req.Model].forTTL(ttl)
	ceiling := llmCeilingCents(req, price)
	r, err := c.ds.reserveHost(ctx, substrate.HostServiceLLM, ceiling)
	if err != nil {
		return nil, err
	}
	res, err := c.inner.Complete(ctx, req, onDelta)
	cents, whole := 0.0, false
	if res != nil && res.Usage != nil {
		cents, whole = price.cost(res.Usage)*100, !res.Usage.Partial
	}
	c.ds.settleCall(ctx, r, err, cents, whole)
	return res, err
}

// hostMaxTokens is the completion ceiling a host-paid request carries: the
// host's where the request names none, and the host's where it names more.
func hostMaxTokens(asked, ceiling int) int {
	if asked <= 0 || asked > ceiling {
		return ceiling
	}
	return asked
}

// llmCeilingCents prices a request at its worst: the estimated prompt at the
// higher of the input and cache-write rates, and every completion token the
// request allows at the output rate.
func llmCeilingCents(req llm.Request, p modelPrice) float64 {
	in := max(p.inPer1M, p.cacheWritePer1M)
	usd := (float64(estimatePromptTokens(req))*in + float64(req.Params.MaxTokens)*p.outPer1M) / 1e6
	return usd * 100
}

// estimatePromptTokens over-estimates the tokens a request's prompt takes.
func estimatePromptTokens(req llm.Request) int {
	n := len(req.System)
	for _, m := range req.Messages {
		n += len(m.Content) + len(m.Context) + len(m.ReasoningBlocks) + len(m.ToolName) + len(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			n += len(tc.Name) + len(tc.Arguments) + len(tc.ID)
		}
	}
	for _, t := range req.Tools {
		n += len(t.Name) + len(t.Description)
		if raw, err := json.Marshal(t.Parameters); err == nil {
			n += len(raw)
		}
	}
	return int(math.Ceil(float64(n)/hostPromptBytesPerToken*hostPromptMargin)) + hostPromptFraming
}

// --- the embeddings gate ------------------------------------------------------

// embedCeilingCents prices an embeddings request at its worst, on the same
// generous estimate a prompt takes.
func embedCeilingCents(texts []string, per1M float64) float64 {
	n := 0
	for _, t := range texts {
		n += len(t)
	}
	tokens := math.Ceil(float64(n)/hostPromptBytesPerToken*hostPromptMargin) + float64(8*len(texts))
	return tokens * per1M / 1e6 * 100
}

// hostEmbed is one embeddings request on the host's entry: reserved, made,
// settled to the tokens the endpoint reported, which EmbedUsage returns beside
// an answer it then refused. A count of zero is no report.
func (p *embedProvider) hostEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	r, err := p.gate.reserveHost(ctx, substrate.HostServiceEmbeddings, embedCeilingCents(texts, p.per1M))
	if err != nil {
		return nil, err
	}
	vecs, usage, err := p.client.EmbedUsage(ctx, texts)
	p.gate.settleCall(ctx, r, err, float64(usage.PromptTokens)*p.per1M/1e6*100, usage.PromptTokens > 0)
	return vecs, err
}

// --- holding a run before it starts --------------------------------------------

// hostHold says why a root run of ag is held at the host's allowance, or ""
// when it may run: the agent's provider row is host-backed and the day's
// spend leaves less than one completion at the most the agent's requests may
// write. A run admitted here can still meet a refusal at a later call whose
// prompt does not fit, which fails that run with the same words.
func (ds *dataset) hostHold(ctx context.Context, ag *vocabulary.Agent) (string, error) {
	if ds.svc.host == nil {
		return "", nil
	}
	id, err := ds.agentProviderID(ctx, ag)
	if err != nil {
		return "", err
	}
	entry, err := ds.hostProviderRow(ctx, id)
	if err != nil || entry == nil {
		return "", err
	}
	pc := hostProviderConfig(entry)
	model := ag.Model
	if ag.ProviderInput != "" || model == "" {
		model = vocabulary.PreferredAgentModel(ag.Models, entry.ModelIDs())
	}
	params, err := mergeParams(entry.ID, entry.Defaults, ag.Params)
	if err != nil {
		// Dispatch refuses it with its own words.
		return "", nil
	}
	floor := float64(hostMaxTokens(params.MaxTokens, entry.MaxOutputTokens)) * pc.pricing[model].outPer1M / 1e6 * 100
	used, _, resets, err := ds.hostSpendToday(ctx)
	if err != nil {
		return "", err
	}
	limit := float64(ds.svc.host.Budget.CentsPerDay)
	if used+floor <= limit+hostBudgetEpsilon {
		return "", nil
	}
	return hostSpendDetail(used, limit, resets), nil
}

// hostHeldPrefix opens a hold's text when it is the host's allowance, not the
// person's cap.
const hostHeldPrefix = "host allowance reached: "

// errHostHeld reports whether a hold's text is the host's allowance.
func errHostHeld(text string) bool { return strings.HasPrefix(text, hostHeldPrefix) }

// hostRefusal is the error a root run held at the host's allowance answers.
func hostRefusal(text string) error {
	return fmt.Errorf("%w: %s", substrate.ErrHostBudget, strings.TrimPrefix(text, hostHeldPrefix))
}

// isHostBudget reports whether err is the host's allowance refusing a call.
func isHostBudget(err error) bool { return errors.Is(err, substrate.ErrHostBudget) }
