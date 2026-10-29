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

/** The id of the repository's daily spend cap: a core `setting` record the
 * engine reads over every agent's runs. Nothing seeds it. */
export const REPOSITORY_SPEND_CAP_ID = `${LLM_PACKAGE}/spendCentsPerDay`

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
