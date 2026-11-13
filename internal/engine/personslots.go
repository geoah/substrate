package engine

import "sync"

// personSlots counts what each person a repository serves holds of a limit
// set per person (substrate.PersonOf): the chat turns one person runs at
// once.
type personSlots struct {
	mu   sync.Mutex
	held map[string]int
}

// take holds one slot of person's, or reports that the person holds limit
// already.
func (p *personSlots) take(person string, limit int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held[person] >= limit {
		return false
	}
	if p.held == nil {
		p.held = map[string]int{}
	}
	p.held[person]++
	return true
}

// give returns one slot of person's.
func (p *personSlots) give(person string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held[person] <= 1 {
		delete(p.held, person)
		return
	}
	p.held[person]--
}
