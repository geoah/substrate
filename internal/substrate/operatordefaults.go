package substrate

import "time"

// The operator is whoever runs the deployment. An operator default is a key or
// an OAuth client the operator supplies, read from the file
// SUBSTRATE_OPERATOR_DEFAULTS names, and used only where the repository holds
// none of its own. It never enters a repository (decision 0232,
// docs/plans/host-defaults.md).

// The slots an operator default can fill, one per place the substrate spends a
// key.
const (
	// SlotLLM is one llm/provider row's apiKey: chat completions and titles.
	SlotLLM = "llm"
	// SlotEmbeddings is the repository's embeddings provider.
	SlotEmbeddings = "embeddings"
	// SlotOAuth is a published provider's OAuth client input.
	SlotOAuth = "oauth"
	// SlotFunction is the secret a published provider's named functions spend.
	SlotFunction = "function"
)

// Where a slot's key comes from now. A key the person wrote always wins over
// the operator's.
const (
	SourceOperator = "operator"
	SourceOwn      = "own"
	SourceNone     = "none"
)

// Whether a slot works, read apart from where its key comes from: a slot the
// operator fills can still need the person to connect an account.
const (
	// ReadinessReady: the key resolves. It does not claim a call has
	// succeeded.
	ReadinessReady = "ready"
	// ReadinessNotSetUp: no key resolves, or the llm/provider row the slot
	// needs was deleted.
	ReadinessNotSetUp = "not-set-up"
	// ReadinessNeedsConnect: an OAuth client resolves and no account holds a
	// grant through it.
	ReadinessNeedsConnect = "needs-connect"
	// ReadinessReconnectNeeded: an account's grant came from a client that is
	// no longer the one resolving, or its refresh was refused.
	ReadinessReconnectNeeded = "reconnect-needed"
	// ReadinessFailing: the newest open alert of something this slot pays
	// for, its summary in Message.
	ReadinessFailing = "failing"
	// ReadinessInstallFailed: the operator listed the package for install and
	// the last attempt failed, its error in Message.
	ReadinessInstallFailed = "install-failed"
)

// OperatorDefaults is `GET /api/v1/operator/defaults`: every slot the
// repository spends a key through, each resolved by the same resolver
// dispatch, the OAuth facility and function injection use, and the operator's
// allowance for today. On a deployment with no operator defaults file
// Operator is absent, every item's source is `own` or `none`, and no item
// says the operator offers it.
type OperatorDefaults struct {
	Operator *OperatorInfo     `json:"operator,omitempty"`
	Items    []OperatorDefault `json:"items"`
}

// OperatorInfo is the operator as the person sees it: the site's name from
// site.yaml (absent where the deployment has no site directory) and today's
// allowance.
type OperatorInfo struct {
	Name      string             `json:"name,omitempty"`
	Allowance *OperatorAllowance `json:"allowance,omitempty"`
}

// OperatorAllowance is what the operator pays for one repository in one UTC
// calendar day: the limit, what its reservations and settled calls hold now,
// split by slot, and when the day ends.
type OperatorAllowance struct {
	CentsPerDay int64     `json:"centsPerDay"`
	UsedCents   int64     `json:"usedCents"`
	ResetsAt    time.Time `json:"resetsAt"`
	// BySlot is UsedCents split by OperatorDefault.Slot, absent where nothing
	// was spent today.
	BySlot []OperatorSpend `json:"bySlot,omitempty"`
}

// OperatorSpend is one slot's share of a day's operator-paid spend.
type OperatorSpend struct {
	Slot  string `json:"slot"`
	Cents int64  `json:"cents"`
}

// OperatorDefault is one slot and where it stands.
type OperatorDefault struct {
	// ID is stable across reads: `llm/<row id>`, `embeddings`,
	// `oauth/<package>`, `function/<package>`.
	ID string `json:"id"`
	// Slot is one of the Slot* words.
	Slot string `json:"slot"`
	// Label is the slot's name as the person reads it.
	Label string `json:"label"`
	// Target is what the slot belongs to: an llm/provider record path, or a
	// package id. For embeddings it is the row whose card shows the search
	// line: the claiming row where the person claimed embedModel, else the
	// row the operator's embeddings entry stands in for; absent when that row
	// does not exist.
	Target string `json:"target,omitempty"`
	// Source is one of the Source* words.
	Source string `json:"source"`
	// OperatorOffers is true when the operator's file serves this slot to
	// this repository, whatever the source, readiness or allowance: removing
	// the person's own key would fall back to the operator's.
	OperatorOffers bool `json:"operatorOffers"`
	// Readiness is one of the Readiness* words.
	Readiness string `json:"readiness"`
	// Message says why, where Readiness is not `ready`.
	Message string `json:"message,omitempty"`
	// Notice is the operator's own words about this slot (an OAuth client in
	// Google's Testing mode, say), absent where the operator wrote none.
	Notice string `json:"notice,omitempty"`
	// Models are the model ids an operator-served llm slot allows, absent
	// where the source is not the operator.
	Models []string `json:"models,omitempty"`
}
