/** An app's scopes in the owner's words (decisions 0194 and 0195): what each
 * verb lets the app do with its pattern, for the approval page and the Apps
 * list in Settings. Pure, so the words are tested here. */

import { agentName } from "@/lib/actor-identity"
import { splitKind } from "@/lib/api/http"
import type {
  AuthorizationKind,
  AuthorizationScope,
  ScopeVerb,
} from "@/lib/api/types"
import { displayPlural, lowerFirst, packageDisplayName } from "@/lib/kind-names"
import { listWords } from "@/lib/tools"

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
  agents: "Talk to, and read the chats of, the agents matching",
  decide: "Apply, edit or dismiss changes suggested by the agents matching",
  webhook: "Send data to, and see the refused deliveries of, the webhook",
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

// ── the everyday sentence ───────────────────────────────────────────────────

/** How many kind labels one sentence names before it counts the rest; the
 * technical view lists every kind by its reference. */
const MAX_LABELS = 6

/** What a sentence reads of a scope: its verb and pattern, and what it
 * reaches today. */
export type SentenceScope = Pick<
  AuthorizationScope,
  "verb" | "pattern" | "glob" | "kinds" | "agents"
>

/** A pattern's shape: every kind, one authority's, one package's, or one
 * kind or agent. */
type PatternShape =
  | { shape: "all" }
  | { shape: "authority"; authority: string }
  | { shape: "package"; authority: string; pkg: string }
  | { shape: "exact"; authority: string; pkg: string; name: string }

function patternShape(pattern: string): PatternShape {
  if (pattern === "*") return { shape: "all" }
  if (pattern.endsWith("/*")) {
    const [authority, pkg] = pattern.slice(0, -2).split("/")
    return pkg
      ? { shape: "package", authority, pkg }
      : { shape: "authority", authority }
  }
  return { shape: "exact", ...splitKind(pattern) }
}

/** "Apple contacts and events", or "tasks and projects" where the package's
 * own name would only repeat the first label or the pattern names none. */
function packagePhrase(pkg: string, labels: string[]): string {
  if (!pkg) return listWords(labels)
  const name = packageDisplayName(pkg)
  const lower = name.toLowerCase()
  const repeats = labels.some((l) =>
    `${l.toLowerCase()} `.startsWith(`${lower} `)
  )
  return repeats ? listWords(labels) : `${name} ${listWords(labels)}`
}

/** The kinds a scope reaches, grouped by package: the primary kinds by their
 * labels, the rest counted, so a sentence names what a person keeps and
 * still says how much more it reaches. */
function kindsPhrase(kinds: readonly AuthorizationKind[]): string {
  const groups = new Map<string, { pkg: string; kinds: AuthorizationKind[] }>()
  for (const kind of kinds) {
    const { authority, pkg } = splitKind(kind.identity)
    const key = `${authority}/${pkg}`
    const group = groups.get(key) ?? { pkg, kinds: [] }
    group.kinds.push(kind)
    groups.set(key, group)
  }
  let budget = MAX_LABELS
  let more = 0
  const phrases: string[] = []
  for (const group of groups.values()) {
    const primary = group.kinds.filter(
      (k) => !k.purpose || k.purpose === "primary"
    )
    const named = (primary.length > 0 ? primary : group.kinds).slice(
      0,
      Math.max(budget, 0)
    )
    more += group.kinds.length - named.length
    budget -= named.length
    if (named.length === 0) continue
    const labels = named.map((k) =>
      lowerFirst(k.label?.plural ?? displayPlural(k.identity))
    )
    phrases.push(packagePhrase(group.pkg, labels))
  }
  const list =
    phrases.length <= 1
      ? (phrases[0] ?? "")
      : `${phrases.slice(0, -1).join(", ")}, and ${phrases[phrases.length - 1]}`
  if (more === 0) return list
  return `${list}, and ${more} more ${more === 1 ? "kind" : "kinds"}`
}

/** What a kind pattern names when it reaches nothing yet. */
function patternPhrase(shape: PatternShape): string {
  switch (shape.shape) {
    case "all":
      return "records"
    case "authority":
      return `records from ${shape.authority}`
    case "package":
      return `${packageDisplayName(shape.pkg)} records`
    case "exact": {
      const plural = lowerFirst(
        displayPlural(`${shape.authority}/${shape.pkg}/${shape.name}`)
      )
      return packagePhrase(shape.pkg, [plural])
    }
  }
}

const KIND_VERBS: Record<"read" | "write", string> = {
  read: "See",
  write: "See, add, change and delete",
}

/** The agents an agents or decide scope names: by name where it matches any
 * today, else by what its pattern covers. */
function agentsPhrase(scope: SentenceScope, shape: PatternShape): string {
  if (shape.shape === "all") return "your agents"
  if (scope.agents.length > 0)
    return listWords(scope.agents.map((a) => agentName(a.id)))
  switch (shape.shape) {
    case "authority":
      return `the agents from ${shape.authority}`
    case "package":
      return `the agents in ${packageDisplayName(shape.pkg)}`
    case "exact":
      return agentName(scope.pattern)
  }
}

/** A scope as the everyday sentence the approval page shows beside its
 * checkbox: what the app may do, over the kinds or the agents it reaches by
 * their labels. Every power stays in the words: a write may delete, a glob
 * reaches what is added later, talking to an agent reaches whatever the
 * agent may do and reads every chat with it, and a decision may edit the
 * change it applies. */
export function scopeSentence(scope: SentenceScope): string {
  const shape = patternShape(scope.pattern)
  switch (scope.verb) {
    case "read":
    case "write": {
      const verb = KIND_VERBS[scope.verb]
      const later = scope.glob ? ", including kinds added later" : ""
      if (shape.shape === "all") return `${verb} all your records${later}`
      const what =
        scope.kinds.length > 0 ? kindsPhrase(scope.kinds) : patternPhrase(shape)
      return `${verb} your ${what}${later}`
    }
    case "agents": {
      const later = scope.glob ? ", including agents added later" : ""
      const one = !scope.glob
      const who =
        shape.shape === "all" ? "all your agents" : agentsPhrase(scope, shape)
      const them = one ? "it" : "them"
      return `Talk to ${who}${later}, and through a chat have ${them} do anything ${one ? "it" : "they"} may do; read every chat with ${them}, yours included, answer ${one ? "its" : "their"} questions, and rename or stop any of those chats`
    }
    case "decide": {
      const later = scope.glob ? ", including agents added later" : ""
      const one = !scope.glob
      return `Apply or dismiss the changes ${agentsPhrase(scope, shape)} ${one ? "suggests" : "suggest"}${later}, and edit one before applying it, as you`
    }
    case "webhook":
      return webhookSentence(scope.pattern)
  }
}

/** What a `webhook` detail lets the app do once it is set up: deliver with
 * its own token, for any account the package's callable accepts, and read
 * every delivery to the trigger that could not be filed, whoever sent it.
 * `packageName` is the catalog name of the package whose trigger it is,
 * where the page knows it. */
export function webhookSentence(trigger: string, packageName?: string): string {
  const parked =
    "and see every delivery to it that could not be filed, whoever sent it"
  if (!packageName)
    return `Send data to your repository through the ${trigger} webhook, ${parked}`
  return `Send data to your repository through the ${packageName} webhook, for any ${packageName} account, ${parked}`
}
