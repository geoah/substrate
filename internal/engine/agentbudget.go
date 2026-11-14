package engine

// The owner's override of one agent's budgets (decision 0314): a record of
// llm/agentbudget naming the agent, each limit it sets replacing the
// declared one of the same name. spend.go reads its daily cap.

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// propAgentBudgetAgent is the reference naming the agent an override is for.
const propAgentBudgetAgent = "agent"

// guardAgentBudgetWrite holds llm/agentbudget to one live record per agent,
// so the record the owner reads is the one that holds and changing a limit
// is one write. It runs inside apply beside the other per-kind write guards,
// after the reference pass made `agent` canonical, and reads the write's own
// reference over the stored one, so a patch that moves a record to another
// agent is held too. A repository has one writer, so the check and the write
// it guards cannot interleave with another write.
func (t *txn) guardAgentBudgetWrite(sp *applySpec) error {
	if sp.ty.Identity != vocabulary.KindLLMAgentBudget {
		return nil
	}
	path := referencePathOf(sp.props[propAgentBudgetAgent])
	if _, named := sp.props[propAgentBudgetAgent]; !named && sp.existing != nil {
		path = referencePathOf(sp.existing.Props[propAgentBudgetAgent])
	}
	if path == "" {
		return nil
	}
	var other string
	err := t.tx.QueryRowContext(t.ctx, `
		SELECT id FROM records
		 WHERE kind = $1 AND deleted_at IS NULL AND id <> $2 AND props->$3->>$4 = $5
		 ORDER BY id LIMIT 1`,
		vocabulary.KindLLMAgentBudget, sp.id, propAgentBudgetAgent, vocabulary.ReferenceValueKey, path).Scan(&other)
	if err == nil {
		return fmt.Errorf("%w: the budgets of %s are already overridden by %s: change that record, or delete it first",
			substrate.ErrConflict, path, vocabulary.RecordPath(vocabulary.KindLLMAgentBudget, other))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}
