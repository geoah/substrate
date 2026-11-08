/** The seeded `core` and `llm` references the console names in code, each
 * declared once and spelled in full (record 0101).
 *
 * This module imports nothing, so lib/api and every page can read it without
 * a cycle. eslint.config.js refuses a `substrate.reamde.dev/core/…` or
 * `substrate.reamde.dev/llm/…` string anywhere else in src: a reference the
 * console needs is built here from the package below, so a renamed or moved
 * kind is one edit. */

/** The authority the substrate publishes its own vocabulary under. */
export const CORE_AUTHORITY = "substrate.reamde.dev"

/** The core package's own word: the middle segment of every core kind
 * reference. */
export const CORE_PACKAGE_NAME = "core"

/** The core package IDENTITY, `<authority>/<package>`: the package whose
 * meta-model collections (`kind`, `bundle`, `agent`, …) the console addresses
 * by name. Every other collection comes out of the registry. */
export const CORE_PACKAGE = `${CORE_AUTHORITY}/${CORE_PACKAGE_NAME}`

/** The llm package's own word: the agent runtime's four data kinds
 * (`provider`, `thread`, `message`, `interaction`) are the SECOND package the
 * binary seeds, out of core so core is not everything (decision record 0077). */
export const LLM_PACKAGE_NAME = "llm"

/** The llm package IDENTITY, `<authority>/<package>`. Seeded like core, so
 * these collections are always present. */
export const LLM_PACKAGE = `${CORE_AUTHORITY}/${LLM_PACKAGE_NAME}`

/** The console package's own word: the record views and preferences the
 * console itself reads, seeded like core and llm (decision record 0166). */
export const CONSOLE_PACKAGE_NAME = "console"

/** The packages under CORE_AUTHORITY that hold the substrate's own state:
 * every kind in them is `internal`. The rest of the authority is the owner's
 * data: the seeded `observations` package and the well-known catalogues
 * (`health`, `labs`, `clinical`, …), whose kinds keep their own purpose
 * (decision records 0192 and 0193). */
export const MACHINERY_PACKAGES: readonly string[] = [
  CORE_PACKAGE_NAME,
  LLM_PACKAGE_NAME,
  CONSOLE_PACKAGE_NAME,
]

/** Whether `<authority>/<package>` is the substrate's own machinery. A kind
 * of `observations` or of a well-known catalogue
 * (`substrate.reamde.dev/labs/labreport`) is the owner's data: it is listed
 * with theirs, under the purpose it declares. */
export function isMachinery(authority: string, pkg: string): boolean {
  return authority === CORE_AUTHORITY && MACHINERY_PACKAGES.includes(pkg)
}

/** The owner's contacts: a person, a business, a group or a bot (decision
 * record 0231). */
export const CONTACT_KIND = `${CORE_PACKAGE}/contact`

/** The facts an owner keeps for their agents (decision record 0197). */
export const MEMORY_KIND = `${LLM_PACKAGE}/memory`

/** The owner's standing instructions to one agent (decision record 0241). */
export const INSTRUCTION_KIND = `${LLM_PACKAGE}/instruction`

/** One model an llm/provider row offers, with its price and window; an
 * agent's `runsOn` names these (decision record 0242). */
export const LLM_MODEL_KIND = `${LLM_PACKAGE}/model`

/** The kinds of a machinery package whose records are the owner's own data:
 * listed under the purpose they declare, in a group of their own, and theirs
 * in every origin. The server's `vocabulary.SeededOwnerData` is this list. */
export const SEEDED_OWNER_DATA_KINDS: readonly string[] = [
  CONTACT_KIND,
  MEMORY_KIND,
  INSTRUCTION_KIND,
]

/** Whether a kind reference is one of the owner-data kinds inside the
 * machinery packages. */
export function isSeededOwnerData(kind: string): boolean {
  return SEEDED_OWNER_DATA_KINDS.includes(kind)
}

/** The machinery packages whose owner-data kinds are listed together, under
 * one name: what the assistant knows about the owner (memories) and how it
 * should work (instructions) are both the assistant's, so they are one
 * place. An owner-data kind of a package not named here is a group of its
 * own, named by its plural. */
export const SHARED_OWNER_DATA_GROUPS: Readonly<Record<string, string>> = {
  [LLM_PACKAGE]: "Assistant",
}

/** The name of the group an owner-data kind shares with its package's
 * others, or undefined for a kind that is a group of its own (or no
 * owner-data kind at all). */
export function sharedOwnerDataGroup(kind: string): string | undefined {
  if (!isSeededOwnerData(kind)) return undefined
  return SHARED_OWNER_DATA_GROUPS[kind.slice(0, kind.lastIndexOf("/"))]
}

/** Whether a kind is the substrate's machinery: a kind of core, llm or
 * console that is not the owner's data. */
export function isMachineryKind(kind: string): boolean {
  const [authority = "", pkg = ""] = kind.split("/")
  return isMachinery(authority, pkg) && !isSeededOwnerData(kind)
}

/** The id of the repository's daily spend cap: a core `setting` record the
 * engine reads over every agent's runs. Nothing seeds it. */
export const REPOSITORY_SPEND_CAP_ID = `${LLM_PACKAGE}/spendCentsPerDay`

/** The id of the core `setting` record whose `value` is the id of the
 * owner's own contact, an ordinary CONTACT_KIND record (decision record
 * 0231). Nothing seeds it: the console writes it once the owner gives their
 * name. */
export const OWNER_CONTACT_SETTING_ID = `${CORE_PACKAGE}/ownerContact`

/** The kind every kind declaration is a record of. A grant entry is a
 * reference at one, so its path starts with this. */
export const KIND_KIND = `${CORE_PACKAGE}/kind`

export const AGENT_KIND = `${CORE_PACKAGE}/agent`

/** The function records. A tool entry and a trigger's `callable` are
 * references at one, so their paths start with this. */
export const FUNCTION_KIND = `${CORE_PACKAGE}/function`

export const TRIGGER_KIND = `${CORE_PACKAGE}/trigger`

/** The trigger run ledger: one row per delivery. */
export const TRIGGER_RUN_KIND = `${CORE_PACKAGE}/triggerrun`

/** The question kind the `ask` host function lands
 * (`vocabulary.KindLLMInteraction`). */
export const INTERACTION_KIND = `${LLM_PACKAGE}/interaction`

/** Where INTERACTION_KIND was before record 0077 moved it out of core. A
 * stamp written before the move still spells it, and the loader still
 * accepts it on a stored grant the boot upgrade has not rewritten yet. */
export const INTERACTION_KIND_PRE_MOVE = `${CORE_PACKAGE}/llminteraction`

/** The core `sync` trait by its full identity, the spelling every host check
 * compares (a package-local look-alike can never answer for it). */
export const SYNC_TRAIT = `${CORE_PACKAGE}/sync`

/** Core's `observed` trait: a kind whose records hold measured values the
 * engine indexes (decision 0154). Its two variants describe layout only, and
 * the console tells them apart by whether the kind declares `values`. */
export const OBSERVED_TRAIT = `${CORE_PACKAGE}/observed`

/** Core's `metric` trait: every metric kind binds it, so the console finds
 * the metric records by it, never by a sample's kind name. */
export const METRIC_TRAIT = `${CORE_PACKAGE}/metric`

/** The four core kinds a grant glob never reaches (`vocabulary.AuthKinds`,
 * record 0080): an entry has to spell one out to grant it. */
export const AUTH_KINDS: readonly string[] = [
  `${CORE_PACKAGE}/token`,
  `${CORE_PACKAGE}/credential`,
  `${CORE_PACKAGE}/secret`,
  `${CORE_PACKAGE}/recoverykey`,
]
