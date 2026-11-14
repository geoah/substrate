package engine

import "sync"

// personSlots counts what each person a repository serves holds of a limit
// set per person (substrate.PersonOf): the chat turns one person runs at
// once. It also keeps what waited for a slot, so a give hands it back in the
// same step that frees the slot and nothing that waited is forgotten.
type personSlots struct {
	mu   sync.Mutex
	held map[string]int
	// waiting are the resumes each person's full slots turned away
	// (deferredResume), at most one per thread.
	waiting map[string][]deferredResume
}

// deferredResume is a chat thread's continuation that found its person's
// slots all taken: the thread, and the resolution row the turn starts from.
type deferredResume struct {
	thread, resolution string
}

// take holds one slot of person's, or reports that the person holds limit
// already.
func (p *personSlots) take(person string, limit int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.takeLocked(person, limit)
}

func (p *personSlots) takeLocked(person string, limit int) bool {
	if p.held[person] >= limit {
		return false
	}
	if p.held == nil {
		p.held = map[string]int{}
	}
	p.held[person]++
	return true
}

// takeOrWait holds one slot of person's, or keeps r to be handed back by the
// give that frees one. The two are one step under the lock: a slot freed
// between a refusal and the wait being kept would hand back nothing, and the
// resume would wait for a give that had already happened.
func (p *personSlots) takeOrWait(person string, limit int, r deferredResume) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.takeLocked(person, limit) {
		return true
	}
	for _, w := range p.waiting[person] {
		if w.thread == r.thread {
			// The continuation replays every row the thread holds, so one
			// wait covers every resolution that lands meanwhile.
			return false
		}
	}
	if p.waiting == nil {
		p.waiting = map[string][]deferredResume{}
	}
	p.waiting[person] = append(p.waiting[person], r)
	return false
}

// give returns one slot of person's, and hands back the resumes that waited
// for one, which the caller starts again.
func (p *personSlots) give(person string) []deferredResume {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.held[person] <= 1 {
		delete(p.held, person)
	} else {
		p.held[person]--
	}
	waited := p.waiting[person]
	delete(p.waiting, person)
	return waited
}

// drainWaiting hands back every resume that waits, whoever it waits for.
func (p *personSlots) drainWaiting() []deferredResume {
	p.mu.Lock()
	defer p.mu.Unlock()
	var all []deferredResume
	for person, waited := range p.waiting {
		all = append(all, waited...)
		delete(p.waiting, person)
	}
	return all
}
