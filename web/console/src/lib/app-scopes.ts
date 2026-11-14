/** An app's scopes in the owner's words (decisions 0194, 0195, 0219 and
 * 0230): what each verb lets the app do with its pattern, for the approval
 * page, the Apps list and the AI assistants page in Settings. Pure, so the
 * words are tested here. */

import { agentName } from "@/lib/actor-identity"
import { LOCAL_AUTHORITY, splitKind } from "@/lib/api/http"
import type {
  AuthorizationCallFunction,
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
  call: "Run the functions matching",
  webhook: "Send data to, and see the refused deliveries of, the webhook",
  full: "Everything you can do when you sign in",
  mcp: "Work with your data as an AI assistant, at this server’s assistant address alone",
}

/** What `full` lets an app do, in every view that names it: the same as
 * signing in, which no other scope comes near. */
export const FULL_ACCESS_SENTENCE =
  "Do everything you can do when you sign in: see, add, change and delete any record, change your kinds, settings and agents, run your functions, and make or revoke tokens, including this one"

/** What `mcp` lets an AI assistant do (decision 0230): what you can, at this
 * server's assistant address and nowhere else, short of the sign-in
 * material, the export, a provider account's connection and a package's
 * removal. */
export const ASSISTANT_ACCESS_SENTENCE =
  "Work with your data as you do, at this server’s AI assistant address and nowhere else, short of tokens and other sign-in material, your export, connecting provider accounts and removing packages"

/** What an assistant can do, one line each. The approval page and Settings'
 * AI assistants page both read these, so the two say the same. */
export const ASSISTANT_CAN = [
  "See everything you can, token and credential records included; a secret’s value reads back hidden",
  "Change anything you can, including collections, agents, triggers and packages",
  "Run your tools, agents and syncs",
] as const

/** What the MCP door refuses an assistant (decision 0230). */
export const ASSISTANT_CANNOT = [
  "Write, make or revoke tokens or other sign-in material",
  "See a webhook’s key",
  "Export your data",
  "Connect a provider account",
  "Uninstall or purge a package",
] as const

/** The owner kept an assistant's reach over guarding the code it declares
 * (decision 0230), so every page that lets one in says this. */
export const ASSISTANT_CODE_SENTENCE =
  "Code it writes runs as your code does, so it can read what your code can, secrets included, and hand them back to it"

/** An assistant writes as its app's actor (decision 0209), never as you. */
export const ASSISTANT_HISTORY_SENTENCE =
  "Its changes show in History under its own name, not yours"

function isVerb(word: string): word is ScopeVerb {
  return Object.hasOwn(ACTIONS, word)
}

/** A stored scope (`agents:~/llm/*`) as words. A scope this console does
 * not know keeps its own spelling as the pattern. */
export function scopeWords(scope: string): ScopeWords {
  if (scope === "full" || scope === "mcp")
    return { verb: scope, action: ACTIONS[scope], pattern: "" }
  const at = scope.indexOf(":")
  const word = at > 0 ? scope.slice(0, at) : ""
  if (!isVerb(word)) return { verb: undefined, action: "", pattern: scope }
  return { verb: word, action: ACTIONS[word], pattern: scope.slice(at + 1) }
}

/** Whether a verb names agents rather than kinds. */
export function overAgents(verb: ScopeVerb): boolean {
  return verb === "agents" || verb === "decide"
}

/** The trait a read or write pattern names (decision 0225),
 * `implements(<trait>)`, or undefined for a kind pattern. */
export function scopeTrait(pattern: string): string | undefined {
  return /^implements\((.+)\)$/.exec(pattern)?.[1]
}

/** A trait by its name, as a sentence says it: `lifecycle`, and
 * `due date` for `dueDate`. */
function traitWord(trait: string): string {
  return splitKind(trait)
    .name.replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .toLowerCase()
}

// ── the everyday sentence ───────────────────────────────────────────────────

/** How many kind labels one sentence names before it counts the rest; the
 * technical view lists every kind by its reference. */
const MAX_LABELS = 6

/** What a sentence reads of a scope: its verb and pattern, and what it
 * reaches today. */
export type SentenceScope = Pick<
  AuthorizationScope,
  "verb" | "pattern" | "glob" | "kinds" | "agents" | "functions"
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
      return shape.authority === LOCAL_AUTHORITY
        ? "records of the kinds you made or added"
        : `records from ${shape.authority}`
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
      return shape.authority === LOCAL_AUTHORITY
        ? "the agents you made or added"
        : `the agents from ${shape.authority}`
    case "package":
      return `the agents in ${packageDisplayName(shape.pkg)}`
    case "exact":
      return agentName(scope.pattern)
  }
}

/** What every read or write scope adds (decision 0219): a semantic search
 * over the kinds it reads embeds the query with the owner's provider. */
const SEMANTIC_CLAUSE =
  ", and search them by meaning, which spends your embedding budget"

/** A scope as the everyday sentence the approval page shows beside its
 * checkbox: what the app may do, over the kinds, the agents or the functions
 * it reaches by their labels. Every power stays in the words: a write may
 * delete, a read searches by meaning at the owner's cost, a glob reaches
 * what is added later, talking to an agent reaches whatever the agent may
 * do and reads every chat with it, a decision may edit the change it
 * applies, and a call runs the owner's code as far as the app already
 * reaches. */
export function scopeSentence(scope: SentenceScope): string {
  const shape = patternShape(scope.pattern)
  switch (scope.verb) {
    case "read":
    case "write": {
      const verb = KIND_VERBS[scope.verb]
      const trait = scopeTrait(scope.pattern)
      if (trait) {
        // A trait scope reaches the kinds that bind the trait when the app
        // asks, so the sentence names the trait, then the kinds that have it
        // today, and says that more may come. A trait is named by a noun as
        // often as by an adjective, so the words never make it either.
        const today =
          scope.kinds.length > 0
            ? `today your ${kindsPhrase(scope.kinds)}`
            : "no kind has it yet"
        return `${verb} your records of every kind with the ${traitWord(trait)} trait (${today}), including kinds that take it on later${SEMANTIC_CLAUSE}`
      }
      const later = scope.glob ? ", including kinds added later" : ""
      if (shape.shape === "all")
        return `${verb} all your records${later}${SEMANTIC_CLAUSE}`
      const what =
        scope.kinds.length > 0 ? kindsPhrase(scope.kinds) : patternPhrase(shape)
      return `${verb} your ${what}${later}${SEMANTIC_CLAUSE}`
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
    case "call":
      return callSentence(scope, shape)
    case "webhook":
      return webhookSentence(scope.pattern)
    case "full":
      return FULL_ACCESS_SENTENCE
    case "mcp":
      return ASSISTANT_ACCESS_SENTENCE
  }
}

/** A function by its name alone, as an agent is: the id's last segment,
 * capitalised. */
export function functionName(id: string): string {
  return agentName(id)
}

/** The functions a call scope names: by name where it matches any today,
 * else by what its pattern covers. */
function functionsPhrase(
  functions: readonly AuthorizationCallFunction[],
  pattern: string,
  shape: PatternShape
): string {
  if (shape.shape === "all") return "any of your functions"
  if (functions.length > 0)
    return listWords(functions.map((f) => functionName(f.identity)))
  switch (shape.shape) {
    case "authority":
      return shape.authority === LOCAL_AUTHORITY
        ? "the functions you made or added"
        : `the functions from ${shape.authority}`
    case "package":
      return `the functions in ${packageDisplayName(shape.pkg)}`
    case "exact":
      return functionName(pattern)
  }
}

/** What a `call` scope lets the app do (decision 0219): run the matching
 * functions, the owner's installed code, each only where the app's other
 * scopes already cover what the function reads and writes. The server
 * refuses a call whose reach they do not cover. */
function callSentence(scope: SentenceScope, shape: PatternShape): string {
  const later = scope.glob ? ", including functions added later" : ""
  const one = !scope.glob
  const who = functionsPhrase(scope.functions ?? [], scope.pattern, shape)
  return `Run ${who}${later}; ${one ? "it runs" : "each runs"} only where the app may already see and change what it reads and writes`
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
