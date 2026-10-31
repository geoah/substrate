/** An app's scopes in the owner's words (decisions 0194 and 0195): what each
 * verb lets the app do with its pattern, for the approval page and the Apps
 * list in Settings. Pure, so the words are tested here. */

import type { ScopeVerb } from "@/lib/api/types"

/** One scope split for display: what it does, and the pattern it does it
 * over. */
export interface ScopeWords {
  verb: ScopeVerb | undefined
  action: string
  pattern: string
}

const ACTIONS: Record<ScopeVerb, string> = {
  read: "Read records of",
  write: "Read and write records of",
  agents: "Talk to the agents matching",
  decide: "Apply or dismiss changes suggested by the agents matching",
}

function isVerb(word: string): word is ScopeVerb {
  return Object.hasOwn(ACTIONS, word)
}

/** A stored scope (`agents:ada.example.com/llm/*`) as words. A scope this
 * console does not know keeps its own spelling as the pattern. */
export function scopeWords(scope: string): ScopeWords {
  const at = scope.indexOf(":")
  const word = at > 0 ? scope.slice(0, at) : ""
  if (!isVerb(word)) return { verb: undefined, action: "", pattern: scope }
  return { verb: word, action: ACTIONS[word], pattern: scope.slice(at + 1) }
}

/** Whether a verb names agents rather than kinds. */
export function overAgents(verb: ScopeVerb): boolean {
  return verb === "agents" || verb === "decide"
}
