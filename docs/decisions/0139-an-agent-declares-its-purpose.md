---
status: accepted
date: 2026-09-28
decision-makers: George Antoniadis
---

# 0139. An agent declares its purpose

## Context and Problem Statement

The console's Agents page lists every agent a repository holds. A fresh
repository seeds six from the LLM sample, and only one of them (`substrate`)
is an agent a person talks to: the rest are demos, sub-agents and the judges a
write policy calls. Each sample adds more. `hiddenFromChat` splits the list,
but it is enforcement (the chat API refuses a marked agent), so it cannot mark
an agent that may be chatted with but should not be offered first. Kinds had
the same problem and answered it with `purpose`
([0133](0133-a-kind-declares-its-purpose.md)).

## Considered Options

- Stars: the owner marks the agents they want listed
- Widen `hiddenFromChat` into a listing hint
- Derive it: an agent named as a sub-agent, or delivered to by a trigger, is a
  helper
- A closed agent key, `purpose: primary | supporting | internal`, the words a
  kind uses

## Decision Outcome

Chosen: the closed key. An agent's `data` may carry `purpose:`: `primary`, an
agent a person chats with; `supporting`, one that works for another agent or a
trigger; `internal`, machinery such as a policy's judge. An absent key reads as
`primary`, so an agent the owner writes is listed without anyone classifying
it. The loader refuses any other value and names the three. As on a kind, the
server stores the key and acts on nothing. The console lists the primary
agents a person can chat with, plus the picked one, and puts the rest behind
"Show more agents". Every shipped agent is classified.

Stars answer "which do I use", not "why does this exist", and the author is
the one who knows the second: a fresh repository would list six agents until
the owner starred one. Widening `hiddenFromChat` would make one flag both
refuse and hide, and an agent a trigger runs is still one a person may open a
chat with. Derivation guesses wrong: `substrateEcho` is nobody's sub-agent and
is still a demo, and a primary agent may also be delivered to by a trigger.
Reusing a kind's words means one meaning of `purpose` across the vocabulary.

### Consequences

- Good, because the sample author says once which agent is the one to talk
  to, and every client reads the same answer.
- Good, because an unclassified agent is listed, never hidden.
- Bad, because the agent key set is closed: a binary older than this one
  refuses a manifest that carries `purpose`, and quarantines the package that
  stores one. It is the cost
  [0020](0020-dialect-keys-are-reserved-not-tolerated.md) names for every new
  key, and the rollback path is the newer binary.
- Bad, because a sample already imported keeps the agents it copied, and they
  read as `primary` until the owner takes the upgrade offer.
- Bad, because the value is a judgment nothing checks.

### Confirmation

`TestAgentPurpose` (internal/vocabulary/agent_test.go) holds the value set,
the refusal and the absent default. `agentListing` in
web/console/src/lib/agent-chat.test.ts holds what the column lists.
