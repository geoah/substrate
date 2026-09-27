/** Where something comes from, in the one set of words every surface uses:
 * "Yours", "From Google", "Made by Notekeeper", "Built into substrate". A
 * tool, a collection and a value each name their origin this way; the mark
 * beside the words is `OriginMark`'s. */

import {
  actorIdentity,
  providerOfKind,
  type ActorIdentity,
  type ProviderInfo,
} from "@/lib/actor-identity"
import { CORE_AUTHORITY, splitKind } from "@/lib/api/http"

export type Origin =
  | { kind: "yours" }
  | { kind: "provider"; provider: ProviderInfo }
  /** An agent, a tool of the repository's own, a package's bundle. */
  | { kind: "actor"; identity: ActorIdentity }
  | { kind: "core" }
  /** Published under an authority that is neither yours nor a provider's. */
  | { kind: "other"; authority: string }

/** Where a value written by `actor` comes from. */
export function originOfActor(actor: string): Origin {
  const identity = actorIdentity(actor)
  if (identity.cls === "you") return { kind: "yours" }
  if (identity.provider)
    return { kind: "provider", provider: identity.provider }
  if (identity.cls === "engine") return { kind: "core" }
  return { kind: "actor", identity }
}

/** Where a kind's records come from: a provider's copy, substrate's own
 * machinery, or yours (samples are imported onto your own authority). */
export function originOfKind(kind: string): Origin {
  const provider = providerOfKind(kind)
  if (provider) return { kind: "provider", provider }
  if (splitKind(kind).authority === CORE_AUTHORITY) return { kind: "core" }
  return { kind: "yours" }
}

/** The origin in words. `short` is a chip's: the name alone ("You",
 * "Google"), where the chip's place already says it is an origin. */
export function originWords(origin: Origin, short = false): string {
  switch (origin.kind) {
    case "yours":
      return short ? "You" : "Yours"
    case "provider":
      return short ? origin.provider.name : `From ${origin.provider.name}`
    case "core":
      return short ? "substrate" : "Built into substrate"
    case "other":
      return short ? origin.authority : `From ${origin.authority}`
    case "actor": {
      const { name, cls } = origin.identity
      if (short) return name
      return cls === "agent" ? `Made by ${name}` : `From ${name}`
    }
  }
}
