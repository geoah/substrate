package engine

// The allowance ledger and its gate (decision 0235). Every call the operator
// pays for, an LLM completion on an operator-served row or an embeddings
// request on the operator's embeddings entry, first RESERVES a conservative
// ceiling in operator_spend_reservations, in one transaction that sums the
// repository's reservations for the UTC calendar day under an advisory lock,
// and is refused when the sum would pass the allowance, the defaults file's
// budget.centsPerDay. The call then SETTLES to its reported cost, failed or
// not. A call that reported no usage, or only part of it, keeps its ceiling
// unless it reported nothing and failed in a way the endpoint cannot have
// billed (egress.ErrUnprocessed), and a reservation still open an hour after
// it opened (a crash, a lost settle) is settled at its ceiling, so the ledger
// only ever overcounts. The ceiling covers one billable attempt, so an
// operator-paid client never retries an attempt that may have been billed
// (llm.Config.RetryOnlyUnbilled).
//
// The gate sits beneath every caller because it wraps the clients
// themselves: operatorLLMClient is the llm.Client every agent loop on an
// operator-served row calls, so its turns, its retries, its compaction and
// its thread title all reserve; embedProvider.Embed reserves for the drain,
// the semantic query and the endpoint check alike. A scoped app token's chat
// and search reach the same clients. Before a run opens, allowanceHold says
// whether the day is already used up, so a trigger is HELD as decision 0149
// holds one at the person's own cap and a direct call is refused before a
// thread opens.
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
	"github.com/geoah/substrate/internal/llm"
	"github.com/geoah/substrate/internal/operatordefaults"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// operatorReservationExpiry is how long a reservation stays open before it
	// is settled at its ceiling.
	operatorReservationExpiry = time.Hour
	// operatorLedgerKeep is how long a row stays in the ledger: the day it
	// was opened in, and a day past it for the operator to read.
	operatorLedgerKeep = 48 * time.Hour
	// operatorSettleTimeout bounds a settle's own write, which runs on a
	// context the call's cancellation does not reach.
	operatorSettleTimeout = 10 * time.Second
	// allowanceEpsilon absorbs a float sum's last bits.
	allowanceEpsilon = 1e-9
)

// allowanceDay is the UTC calendar day now falls in, and when it ends.
func allowanceDay(now time.Time) (start, end time.Time) {
	start = now.UTC().Truncate(24 * time.Hour)
	return start, start.Add(24 * time.Hour)
}

// operatorReservation is one open reservation.
type operatorReservation struct {
	id      int64
	ceiling float64
}

// allowanceRefusal is the error a call past the allowance answers.
func (ds *dataset) allowanceRefusal(used, limit float64, resets time.Time) error {
	return fmt.Errorf("%w: %s", substrate.ErrAllowance, ds.allowanceDetail(used, limit, resets))
}

// allowanceDetail names the site, what is left of today's allowance in whole
// cents rounded down, when it resets and the way out. Calls still running
// count at their ceiling, so "used up" can come back as the day's calls
// settle. It opens with allowancePrefix, which is how a hold's text is told
// from the person's own cap.
func (ds *dataset) allowanceDetail(used, limit float64, resets time.Time) string {
	state := "is used up"
	if left := int64(math.Floor(limit - used + allowanceEpsilon)); left > 0 {
		state = fmt.Sprintf("has %d of %d cents left, not enough for the next call", left, int64(limit))
	}
	return fmt.Sprintf("%s%s %s; it resets at %s UTC; add your own key to keep going",
		allowancePrefix, ds.siteName(), state, resets.UTC().Format("15:04"))
}

// reserveOperator reserves ceiling cents for one operator-paid call of slot,
// or refuses with ErrAllowance. Reservations of one repository serialize on
// an advisory lock, so two calls cannot both find room for one.
func (ds *dataset) reserveOperator(ctx context.Context, slot string, ceiling float64) (*operatorReservation, error) {
	limit := float64(ds.svc.operatorDefaults.Budget.CentsPerDay)
	now := ds.svc.operatorNow().UTC()
	dayStart, dayEnd := allowanceDay(now)
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve operator spend: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey("operatorspend")); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve operator spend: %w", err)
	}
	if err := expireOperatorReservations(ctx, tx, now); err != nil {
		return nil, err
	}
	used, err := operatorSpendSince(ctx, tx, dayStart)
	if err != nil {
		return nil, err
	}
	if used+ceiling > limit+allowanceEpsilon {
		return nil, ds.allowanceRefusal(used, limit, dayEnd)
	}
	r := &operatorReservation{ceiling: ceiling}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO operator_spend_reservations (slot, opened_at, ceiling_cents)
		VALUES ($1, $2, $3::float8) RETURNING id`, slot, now, ceiling).Scan(&r.id); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve operator spend: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("substrate/engine: reserve operator spend: %w", err)
	}
	return r, nil
}

// expireOperatorReservations settles every reservation open past
// operatorReservationExpiry at its ceiling, and drops rows older than
// operatorLedgerKeep.
func expireOperatorReservations(ctx context.Context, q dbx, now time.Time) error {
	if _, err := q.ExecContext(ctx, `
		UPDATE operator_spend_reservations SET settled_cents = ceiling_cents, settled_at = $1
		 WHERE settled_at IS NULL AND opened_at <= $2`, now, now.Add(-operatorReservationExpiry)); err != nil {
		return fmt.Errorf("substrate/engine: expire operator reservations: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM operator_spend_reservations WHERE opened_at < $1`, now.Add(-operatorLedgerKeep)); err != nil {
		return fmt.Errorf("substrate/engine: prune operator reservations: %w", err)
	}
	return nil
}

// operatorSpendSince sums the reservations opened at or after since: each at
// its settled cost, or at its ceiling while it is open.
func operatorSpendSince(ctx context.Context, q dbx, since time.Time) (float64, error) {
	var used float64
	if err := q.QueryRowContext(ctx, `
		SELECT coalesce(sum(coalesce(settled_cents, ceiling_cents)), 0)::float8
		  FROM operator_spend_reservations WHERE opened_at >= $1`, since).Scan(&used); err != nil {
		return 0, fmt.Errorf("substrate/engine: read operator spend: %w", err)
	}
	return used, nil
}

// settleOperator records a reservation's cost. A reservation already settled
// (it expired first) keeps what it holds, so a late settle never lowers a
// ceiling the expiry charged. It runs on a context the call's own
// cancellation does not reach: a stopped turn still settles.
func (ds *dataset) settleOperator(ctx context.Context, r *operatorReservation, cents float64) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), operatorSettleTimeout)
	defer cancel()
	if cents < 0 || math.IsNaN(cents) || math.IsInf(cents, 0) {
		cents = r.ceiling
	}
	if _, err := ds.db.ExecContext(ctx, `
		UPDATE operator_spend_reservations SET settled_cents = $2::float8, settled_at = $3
		 WHERE id = $1 AND settled_at IS NULL`, r.id, cents, ds.svc.operatorNow().UTC()); err != nil {
		// The row stays open and the expiry settles it at its ceiling.
		ds.svc.log.Warn("substrate: an operator-paid call's settle was not written; its ceiling stands",
			"repository", ds.info.ID, "reservation", r.id, "error", err)
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
func (ds *dataset) settleCall(ctx context.Context, r *operatorReservation, callErr error, cents float64, whole bool) {
	switch {
	case whole:
		ds.settleOperator(ctx, r, cents)
	case cents == 0 && errors.Is(callErr, egress.ErrUnprocessed):
		ds.settleOperator(ctx, r, 0)
	default:
		ds.settleOperator(ctx, r, max(cents, r.ceiling))
	}
}

// operatorSpendToday is the day's spend in cents, split by slot.
func (ds *dataset) operatorSpendToday(ctx context.Context) (used float64, bySlot map[string]float64, resets time.Time, err error) {
	start, end := allowanceDay(ds.svc.operatorNow())
	rows, err := ds.db.QueryContext(ctx, `
		SELECT slot, coalesce(sum(coalesce(settled_cents, ceiling_cents)), 0)::float8
		  FROM operator_spend_reservations WHERE opened_at >= $1 GROUP BY slot ORDER BY slot`, start)
	if err != nil {
		return 0, nil, end, fmt.Errorf("substrate/engine: read operator spend: %w", err)
	}
	defer func() { _ = rows.Close() }()
	bySlot = map[string]float64{}
	for rows.Next() {
		var slot string
		var cents float64
		if err := rows.Scan(&slot, &cents); err != nil {
			return 0, nil, end, err
		}
		bySlot[slot] = cents
		used += cents
	}
	return used, bySlot, end, rows.Err()
}

// --- the LLM gate -----------------------------------------------------------

// Prompt estimation for a reservation. The ceiling must not be beaten by the
// call it covers, so the estimate is generous: a token per three bytes, a
// quarter more on top, and a fixed allowance for the wire's own framing.
const (
	promptBytesPerToken = 3
	promptMargin        = 1.25
	promptFraming       = 256
)

// operatorLLMClient is the client every call on an operator-served row goes
// through: it holds the request to the operator's model list and output
// ceiling, reserves before the call and settles after.
type operatorLLMClient struct {
	ds    *dataset
	entry *operatordefaults.LLM
	price map[string]modelPrice
	inner llm.Client
}

func newOperatorLLMClient(ds *dataset, pc *providerConfig, inner llm.Client) llm.Client {
	return &operatorLLMClient{ds: ds, entry: pc.operator, price: pc.pricing, inner: inner}
}

func (c *operatorLLMClient) Complete(ctx context.Context, req llm.Request, onDelta func(llm.Delta)) (*llm.Result, error) {
	if _, ok := c.entry.Model(req.Model); !ok {
		return nil, fmt.Errorf("%w: llm/provider row %q runs on %s's key, which does not serve model %q: name one of %s, or add your own key to the row to use any model",
			substrate.ErrValidation, c.entry.ID, c.ds.siteName(), req.Model, strings.Join(c.entry.ModelIDs(), ", "))
	}
	req.Params.MaxTokens = operatorMaxTokens(req.Params.MaxTokens, c.entry.MaxOutputTokens)
	ttl := ""
	if req.Cache != nil {
		ttl = req.Cache.TTL
	}
	price := c.price[req.Model].forTTL(ttl)
	ceiling := llmCeilingCents(req, price)
	r, err := c.ds.reserveOperator(ctx, substrate.SlotLLM, ceiling)
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

// operatorMaxTokens is the completion ceiling an operator-paid request
// carries: the operator's where the request names none, and the operator's
// where it names more.
func operatorMaxTokens(asked, ceiling int) int {
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
	return int(math.Ceil(float64(n)/promptBytesPerToken*promptMargin)) + promptFraming
}

// --- the embeddings gate ------------------------------------------------------

// embedCeilingCents prices an embeddings request at its worst, on the same
// generous estimate a prompt takes.
func embedCeilingCents(texts []string, per1M float64) float64 {
	n := 0
	for _, t := range texts {
		n += len(t)
	}
	tokens := math.Ceil(float64(n)/promptBytesPerToken*promptMargin) + float64(8*len(texts))
	return tokens * per1M / 1e6 * 100
}

// operatorEmbed is one embeddings request on the operator's entry: reserved,
// made, settled to the tokens the endpoint reported, which EmbedUsage returns
// beside an answer it then refused. A count of zero is no report.
func (p *embedProvider) operatorEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	r, err := p.gate.reserveOperator(ctx, substrate.SlotEmbeddings, embedCeilingCents(texts, p.per1M))
	if err != nil {
		return nil, err
	}
	vecs, usage, err := p.request(ctx, texts)
	p.gate.settleCall(ctx, r, err, float64(usage.PromptTokens)*p.per1M/1e6*100, usage.PromptTokens > 0)
	return vecs, err
}

// --- holding a run before it starts --------------------------------------------

// allowanceHold says why a root run of ag is held at the allowance, or ""
// when it may run. threadID is the thread the run continues, "" for a new
// one, since an agent on runsOn keeps a continued thread on its own row. It
// holds where the row the run takes is operator-served and the day's
// spend leaves less than one completion at the most the agent's requests may
// write. A run admitted here can still meet a refusal at a later call whose
// prompt does not fit, which fails that run with the same words.
func (ds *dataset) allowanceHold(ctx context.Context, ag *vocabulary.Agent, threadID string) (string, error) {
	if ds.svc.operatorDefaults == nil {
		return "", nil
	}
	id, model, err := ds.agentRowAndModel(ctx, ag, threadID)
	if err != nil {
		return "", err
	}
	entry, err := ds.operatorProviderRow(ctx, id)
	if err != nil || entry == nil {
		return "", err
	}
	pc := operatorProviderConfig(entry)
	if model == "" {
		model = vocabulary.PreferredAgentModel(ag.Models, entry.ModelIDs())
	}
	params, err := mergeParams(entry.ID, entry.Defaults, ag.Params)
	if err != nil {
		// Dispatch refuses it with its own words.
		return "", nil
	}
	floor := float64(operatorMaxTokens(params.MaxTokens, entry.MaxOutputTokens)) * pc.pricing[model].outPer1M / 1e6 * 100
	used, _, resets, err := ds.operatorSpendToday(ctx)
	if err != nil {
		return "", err
	}
	limit := float64(ds.svc.operatorDefaults.Budget.CentsPerDay)
	if used+floor <= limit+allowanceEpsilon {
		return "", nil
	}
	return ds.allowanceDetail(used, limit, resets), nil
}

// allowancePrefix opens every allowance text, so a hold's text is told from
// the person's own cap, whose texts open with "spend cap".
const allowancePrefix = "today's allowance from "

// isAllowanceHold reports whether a hold's text is the allowance.
func isAllowanceHold(text string) bool { return strings.HasPrefix(text, allowancePrefix) }

// allowanceHoldRefusal is the error a root run held at the allowance answers.
func allowanceHoldRefusal(text string) error {
	return fmt.Errorf("%w: %s", substrate.ErrAllowance, text)
}

// isAllowance reports whether err is the allowance refusing a call.
func isAllowance(err error) bool { return errors.Is(err, substrate.ErrAllowance) }
