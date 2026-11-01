package substrate

// The agent loop's wire shapes: what the call API returns
// and what the chat stream carries. Conversation state itself is llm/thread +
// llm/message RECORDS in substrate.reamde.dev/llm — these types are only the live
// transport around one invocation.
//
// ALPHA (StabilityAlpha, stability.go). The agent kind, the agent-loop
// vocabulary (llm/provider/llm/thread/llm/message) and the agent call and chat routes
// may change or be superseded with no notice at all. That is a weaker promise
// than the rest of this package makes, and the rest is not frozen either: no
// surface is stable yet, so every other feature reports beta and announces its
// breaks. Everything an agent does is layerable over the function+trigger+effect
// core, so the loop is shipped host-side for the streaming path and is the
// first thing that would be dropped.

// AgentStability is the declared stability of the agent kind, its
// llm/provider/llm/thread/llm/message vocabulary and the agent call and chat routes: "alpha".
// It is the cheap machine-visible marker the discovery/features surface
// reads to mark agents alpha; StabilityAlpha (stability.go) is the value it
// carries.
const (
	// AgentStability marks the agent kind + its core vocabulary + /agents as
	// alpha.
	AgentStability = StabilityAlpha
	// FeatureAgents is the discovery feature key the agent surface is listed
	// under, carrying AgentStability as its stability.
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
	// Status is ok, overbudget, error or stopped: the thread row's settled
	// status. A chat turn a client stopped, or one a server shutdown ended,
	// settles stopped.
	Status string `json:"status"`
	// Reason names the budget that ended an overbudget run, or who or what
	// stopped a stopped one.
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
	// Mentions are the records the final reply links, as the assistant row
	// stores them; absent when it links none.
	Mentions []AgentMention `json:"mentions,omitempty"`
}

// AgentMention is one record a reply links as
// `[text](substrate://<kind>/<id>)`, read by the engine when the run settles.
type AgentMention struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Text is the link's label as the model wrote it.
	Text string `json:"text"`
	// Missing is set when no live record has that kind and id: the model
	// named a record that does not exist, and a client shows the label alone.
	Missing bool `json:"missing,omitempty"`
	// Unread is set when nothing in the run's context carried that record:
	// no tool result and no message. The link is still rendered.
	Unread bool `json:"unread,omitempty"`
}

// The AgentEvent kinds a streaming client sees, in arrival order: deltas
// while an assistant turn streams, tool lifecycle around each dispatch, and
// one done event carrying the AgentResult.
const (
	AgentEventThread       = "thread"
	AgentEventDelta        = "delta"
	AgentEventToolStarted  = "toolStarted"
	AgentEventToolFinished = "toolFinished"
	// AgentEventCompacted reports a summary row the loop wrote over older
	// history: the thread, the context size that triggered it, and how many
	// message rows it replaces in replay.
	AgentEventCompacted = "compacted"
	// AgentEventTruncated marks where an attach's replay lost events: the
	// running turn emitted more than its replay buffer holds, so the events
	// after this line are live and the dropped ones are read from the
	// thread's `llm/message` rows.
	AgentEventTruncated = "truncated"
	// AgentEventIdle is an attach's whole answer when no detached turn runs
	// on the thread: the thread and its row's status, then the stream ends.
	AgentEventIdle = "idle"
	AgentEventDone = "done"
	// AgentEventError terminates a stream that already sent its 200: the loop
	// failed after the status line was gone, so the failure travels as its own
	// event rather than masquerading as a done with no result.
	AgentEventError = "error"

	// AgentEventReasoning is a streamed piece of the model's thinking, in Text,
	// ahead of the turn's deltas; only from a model and wire that expose it.
	AgentEventReasoning = "reasoning"
)

// AgentEvent is one streamed loop event (the chat transport; ndjson on the
// wire, one object per line like the changes feed).
type AgentEvent struct {
	Kind string `json:"kind"`
	// Thread rides the first event: the thread id, minted or continued.
	Thread string `json:"thread,omitempty"`
	// Text is a streamed content delta, or a reasoning piece.
	Text string `json:"text,omitempty"`
	// Tool/Args/OK shape the tool lifecycle events, and ID is the tool call
	// they belong to: a turn may dispatch the same tool twice, so the NAME
	// cannot pair a started event with its finished one — only the id can.
	// It is the same id the `llm/message` transcript keys its tool rows by
	// (`toolCallId`), so a live card and its replayed row are the same card.
	ID   string `json:"id,omitempty"`
	Tool string `json:"tool,omitempty"`
	// Callable rides both tool events: the identity behind the Tool alias, in
	// the actor spelling the `llm/message` rows carry as `callable`, so a live
	// card resolves what ran and not what the alias means on the agent now.
	// Empty for a name the agent carries no tool for.
	Callable string `json:"callable,omitempty"`
	Args     string `json:"args,omitempty"`
	OK       *bool  `json:"ok,omitempty"`
	// Output rides the finished event: the dispatched call's result payload,
	// verbatim, as the tool row stores it. Named apart from Result because
	// that one is the whole run's tally.
	Output string `json:"output,omitempty"`
	// Result rides the done event.
	Result *AgentResult `json:"result,omitempty"`
	// Error rides the error event: a post-200 loop failure the client routes to
	// its error path instead of settling a blank assistant turn.
	Error string `json:"error,omitempty"`
	// TokensBefore and Covered ride the compacted event: the context size in
	// tokens that triggered the compaction, and the number of message rows
	// the new summary row covers.
	TokensBefore int `json:"tokensBefore,omitempty"`
	Covered      int `json:"covered,omitempty"`
	// Turn rides delta, toolStarted and toolFinished: the `turn` ordinal the
	// `llm/message` row for the event carries or will carry. A client that
	// attaches after reading the rows drops every replayed event at or below
	// the highest turn it holds.
	Turn int `json:"turn,omitempty"`
	// Status rides the idle event: the thread row's status.
	Status string `json:"status,omitempty"`
}
