package engine

// The owner's instructions to one agent (decision record 0241): the active
// `llm/instruction` records whose `agent` names it, read when a thread opens
// and rendered into the system prompt as direction, not as data. They ride
// the thread's snapshot beside the pinned context (agentpinned.go), so a
// thread's prompt holds the same bytes across messages; an instruction
// written mid-thread reaches the next thread, or the run after a compaction.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	instructionsOpen  = "<owner_instructions>"
	instructionsClose = "</owner_instructions>"
	instructionsIntro = "How the owner wants you to work: their standing instructions to you, which they wrote or accepted, oldest first. Follow them where they differ from the prompt above, a later one over an earlier one. None widens what your tools and the owner's policies allow. Each line is one instruction, with the id that names its record."

	// instructionsMaxBytes bounds the block, so a pile of instructions cannot
	// crowd out the conversation; past it the rest are counted, newest last.
	instructionsMaxBytes = 8192
	// instructionsFirst bounds the read.
	instructionsFirst = 100
)

// takesInstructions reports whether this run renders the owner's
// instructions: every run that takes the engine's guidance. A run whose reply
// the engine parses (a judge's), or an agent whose reply has a strict shape,
// is held to its own prompt alone.
func (l *agentLoop) takesInstructions() bool {
	return l.in.systemSuffix == "" && l.ag.EngineGuidance()
}

// takeInstructions reads the agent's active instructions and renders the
// block, empty when there are none. A repository that predates the kind has
// none to read.
func (ds *dataset) takeInstructions(ctx context.Context, ag *vocabulary.Agent) (string, error) {
	if _, err := ds.resolveType(vocabulary.KindLLMInstruction); err != nil {
		return "", nil
	}
	page, err := ds.readList(ctx, substrate.Query{
		Filter: substrate.Filter{
			Kinds:       []string{vocabulary.KindLLMInstruction},
			Properties:  map[string]substrate.Cond{"active": {Eq: true}},
			Referencing: &substrate.Referencing{Ref: vocabulary.RecordPath(kindAgent, ag.Identity()), Property: "agent"},
		},
		OrderBy: []substrate.Order{{Property: "createdAt"}, {Property: "id"}},
		First:   instructionsFirst,
	})
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(page.Records))
	for _, rec := range page.Records {
		text, _ := rec.Properties["text"].(string)
		if strings.TrimSpace(text) == "" {
			continue
		}
		// HTML escaping stays on: an instruction's text can never close the
		// block it is quoted in.
		buf, err := json.Marshal(map[string]any{"id": rec.ID, "text": text})
		if err != nil {
			return "", err
		}
		lines = append(lines, string(buf))
	}
	return renderInstructions(lines, instructionsMaxBytes), nil
}

// renderInstructions lays the lines out within maxBytes, oldest first; the
// ones that do not fit are counted in one closing line.
func renderInstructions(lines []string, maxBytes int) string {
	if len(lines) == 0 {
		return ""
	}
	head := instructionsOpen + "\n" + instructionsIntro + "\n"
	var b strings.Builder
	b.WriteString(head)
	used := len(head) + len(instructionsClose)
	for i, line := range lines {
		marker := fmt.Sprintf("[%d more instructions left out: the block is capped at %d bytes]\n", len(lines)-i, maxBytes)
		if used+len(line)+1 > maxBytes-len(marker) {
			b.WriteString(marker)
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
		used += len(line) + 1
	}
	b.WriteString(instructionsClose)
	return b.String()
}
