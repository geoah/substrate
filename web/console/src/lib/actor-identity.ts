/** Who did something, in plain words: "You", an agent by its name, a
 * provider's sync by the provider, the substrate itself. The raw actor string
 * (decision 0025's grammar) is never shortened away: it rides the hover card,
 * and technical mode shows it inline. */

import { CORE_PACKAGE } from "@/lib/api/http"
import { packageDisplayName } from "@/lib/kind-names"

/** The authority shipped providers publish under. */
export const PROVIDERS_AUTHORITY = "providers.substrate.reamde.dev"

export interface ProviderInfo {
  /** The provider's package word (`google`). */
  key: string
  name: string
  letter: string
  /** A CSS colour that reads on both themes. */
  color: string
}

const BRANDS: Record<string, { name?: string; color: string }> = {
  google: { color: "#4285F4" },
  linear: { color: "#5E6AD2" },
  github: { name: "GitHub", color: "var(--foreground)" },
  notion: { color: "var(--foreground)" },
  slack: { color: "#E01E5A" },
  beeper: { color: "#6D5DFC" },
  whoop: { color: "#1CB5AC" },
}

function capitalise(text: string): string {
  return text ? text[0].toUpperCase() + text.slice(1) : text
}

export function providerInfo(key: string): ProviderInfo {
  const brand = BRANDS[key]
  const name = brand?.name ?? packageDisplayName(key)
  return {
    key,
    name,
    letter: name.slice(0, 1).toUpperCase(),
    color: brand?.color ?? "var(--muted-foreground)",
  }
}

/** A kind's provider, when a shipped provider publishes it. */
export function providerOfKind(kind: string): ProviderInfo | undefined {
  const [authority, pkg] = kind.split("/")
  return authority === PROVIDERS_AUTHORITY && pkg
    ? providerInfo(pkg)
    : undefined
}

export type ActorClass = "you" | "agent" | "function" | "bundle" | "engine"

export interface ActorIdentity {
  /** The actor string as stored. */
  actor: string
  cls: ActorClass
  /** The plain name the page shows. */
  name: string
  /** For "You": the door the write came through ("this console"). */
  via?: string
  /** The provider a function or bundle acts for. */
  provider?: ProviderInfo
  /** One sentence for the hover card. */
  description: string
  /** The declaration record behind the actor, where it has one. */
  record?: { kind: string; id: string }
}

const DOORS: Record<string, string> = {
  console: "this console",
  substratectl: "the command line",
  api: "the API",
}

/** A provider function's name: `synccontacts` → "Google Contacts sync",
 * `linearsync` → "Linear sync". */
function functionName(provider: ProviderInfo, name: string): string {
  const own = name.startsWith(provider.key)
    ? name.slice(provider.key.length)
    : name
  if (!own.startsWith("sync") && !own.endsWith("sync"))
    return `${provider.name} ${own}`
  const subject = own.startsWith("sync") ? own.slice(4) : own.slice(0, -4)
  return subject
    ? `${provider.name} ${capitalise(subject)} sync`
    : `${provider.name} sync`
}

/** An agent's name as a person reads it: the local name, camelCase split into
 * words, the first capitalised (`substrateEditor` → "Substrate editor"). The
 * id stays the identity; this is a label. */
export function agentName(id: string): string {
  const local = id.slice(Math.max(id.lastIndexOf("/"), id.lastIndexOf(":")) + 1)
  const spaced = local
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]+/g, " ")
    .toLowerCase()
  return capitalise(spaced.trim() || id)
}

export function actorIdentity(actor: string): ActorIdentity {
  if (actor === "substrate") {
    return {
      actor,
      cls: "engine",
      name: "Substrate",
      description: "The substrate itself: merges, schedules and housekeeping.",
    }
  }
  const parts = actor.split(":")
  const [head, authority, pkg, name] = parts
  const provider =
    authority === PROVIDERS_AUTHORITY && pkg ? providerInfo(pkg) : undefined
  if (head === "agent" && parts.length === 4) {
    return {
      actor,
      cls: "agent",
      name: agentName(name),
      description: "One of your agents.",
      record: {
        kind: `${CORE_PACKAGE}/agent`,
        id: `${authority}/${pkg}/${name}`,
      },
    }
  }
  if (head === "function" && parts.length === 4) {
    return {
      actor,
      cls: "function",
      name: provider ? functionName(provider, name) : name,
      provider,
      description: provider
        ? `Runs for ${provider.name}, a provider you added.`
        : "One of your tools.",
      record: {
        kind: `${CORE_PACKAGE}/function`,
        id: `${authority}/${pkg}/${name}`,
      },
    }
  }
  // The seeded packages install under a bare package word (`bundle:core`).
  if (head === "bundle" && parts.length === 2) {
    return {
      actor,
      cls: "bundle",
      name: "Substrate",
      description: "The substrate's own built-in package.",
    }
  }
  if (head === "bundle" && parts.length === 3) {
    return {
      actor,
      cls: "bundle",
      name: provider ? provider.name : `${pkg} bundle`,
      provider,
      description: provider
        ? "A provider you added."
        : "A bundle this repository installed.",
      record: { kind: `${CORE_PACKAGE}/bundle`, id: `${authority}/${pkg}` },
    }
  }
  // An authority-shaped name is a single-writer bundle acting under its
  // authority's name (record 60), or a provider's package under it.
  if (!actor.includes(":") && actor.includes(".")) {
    const [owner, pkgWord] = actor.split("/")
    const named =
      owner === PROVIDERS_AUTHORITY && pkgWord
        ? providerInfo(pkgWord)
        : undefined
    return {
      actor,
      cls: "bundle",
      name: named ? named.name : actor,
      provider: named,
      description: named
        ? "A provider you added."
        : "A bundle acting under its authority's name.",
    }
  }
  // Every other name is one a request asserted, and a request carries one of
  // this repository's tokens: it is the person who owns them.
  return {
    actor,
    cls: "you",
    name: "You",
    via: DOORS[actor] ?? actor,
    description: "You, signed in.",
  }
}
