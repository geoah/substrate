package substrate

// The agent loop's wire shapes (primitives §5): what the call API returns
// and what the chat stream carries. Conversation state itself is llmthread +
// llmmessage RECORDS in core.substrate.reamde.dev — these types are only the live
// transport around one invocation.
//
// ALPHA, UNFROZEN AT v1. Unlike the rest of this package, the
// agent kind, the agent-loop vocabulary (llmprovider/llmthread/llmmessage) and the
// /agents chat+call wire are NOT part of the frozen v1 contract: they may
// change or be superseded without a v1 wire break. Everything an agent does
// is layerable over the frozen function+trigger+effect core, so the loop is
// shipped host-side for the streaming path but held OUT of the freeze.

// AgentStability is the declared stability of the agent kind, its
// llmprovider/llmthread/llmmessage vocabulary and the /agents wire: "alpha" at v1.
// It is the cheap machine-visible marker the discovery/features surface
// reads to mark agents alpha; StabilityAlpha is the value it
// carries.
const (
	StabilityAlpha  = "alpha"
	StabilityStable = "stable"
	// AgentStability marks the agent kind + its core vocabulary + /agents as
	// alpha.
	AgentStability = StabilityAlpha
	// FeatureAgents is the discovery feature key ticket 005 lists the agent
	// surface under, carrying AgentStability as its stability.
	FeatureAgents = "agents"
)

// AgentResult is one settled agent invocation: the final reply, the thread
// that recorded it, and the rolled-up tally (sub-agents included).
type AgentResult struct {
	// Reply is the assistant's final tool-free turn; empty when a budget
	// ended the loop first.
	Reply string `json:"reply"`
	// Thread is the root thread record's id.
	Thread string `json:"thread"`
	// Status is ok, overbudget or error — the thread row's settled status.
	Status string `json:"status"`
	// Reason names the budget that ended an overbudget run.
	Reason string `json:"reason,omitempty"`
	// Effects counts the writes the run applied, tool calls included;
	// EffectsByAction breaks the count down for the run ledger (root only).
	Effects         int            `json:"effects"`
	EffectsByAction map[string]int `json:"effectsByAction,omitempty"`
	// Turns and ToolCalls are the root loop's own counters.
	Turns     int `json:"turns"`
	ToolCalls int `json:"toolCalls"`
	// The token tally, sub-agents rolled up.
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	CostUSD          float64 `json:"costUSD"`
}

// The AgentEvent kinds a streaming client sees, in arrival order: deltas
// while an assistant turn streams, tool lifecycle around each dispatch, and
// one done event carrying the AgentResult.
const (
	AgentEventThread       = "thread"
	AgentEventDelta        = "delta"
	AgentEventToolStarted  = "toolStarted"
	AgentEventToolFinished = "toolFinished"
	AgentEventDone         = "done"
	// AgentEventError terminates a stream that already sent its 200: the loop
	// failed after the status line was gone, so the failure travels as its own
	// event rather than masquerading as a done with no result.
	AgentEventError = "error"
)

// AgentEvent is one streamed loop event (the chat transport; ndjson on the
// wire, one object per line like the changes feed).
type AgentEvent struct {
	Kind string `json:"kind"`
	// Thread rides the first event: the thread id, minted or continued.
	Thread string `json:"thread,omitempty"`
	// Text is a streamed content delta.
	Text string `json:"text,omitempty"`
	// Tool/Args/OK shape the tool lifecycle events, and ID is the tool call
	// they belong to: a turn may dispatch the same tool twice, so the NAME
	// cannot pair a started event with its finished one — only the id can.
	// It is the same id the `llmmessage` transcript keys its tool rows by
	// (`toolCallId`), so a live card and its replayed row are the same card.
	ID   string `json:"id,omitempty"`
	Tool string `json:"tool,omitempty"`
	Args string `json:"args,omitempty"`
	OK   *bool  `json:"ok,omitempty"`
	// Output rides the finished event: the dispatched call's result payload,
	// verbatim, as the tool row stores it. Named apart from Result because
	// that one is the whole run's tally.
	Output string `json:"output,omitempty"`
	// Result rides the done event.
	Result *AgentResult `json:"result,omitempty"`
	// Error rides the error event: a post-200 loop failure the client routes to
	// its error path instead of settling a blank assistant turn.
	Error string `json:"error,omitempty"`
}
