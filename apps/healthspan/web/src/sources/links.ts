// One device by several routes: an `observations/sourcelink` record names
// the sources that are one device, which a metric read under the priority
// policy takes once (docs/observations.md, One device by several routes).

import type { SubstrateRecord } from "@substrate/client"
import { KIND } from "../kinds"
import { refPath } from "../import/records"

export interface SourceLink {
  id: string
  version: number
  name: string
  /** The linked sources by id. */
  sources: string[]
}

export function readLink(r: SubstrateRecord): SourceLink {
  const prefix = `${KIND.source}/`
  const raw = r.properties.sources
  const sources = (Array.isArray(raw) ? raw : [])
    .map(refPath)
    .filter((p): p is string => !!p && p.startsWith(prefix))
    .map((p) => p.slice(prefix.length))
  const name = r.properties.name
  return {
    id: r.id,
    version: r.version,
    name: typeof name === "string" && name ? name : "Linked sources",
    sources,
  }
}

/** A link's properties as written. */
export function linkProps(
  name: string,
  sources: readonly string[]
): Record<string, unknown> {
  return {
    name: name.trim(),
    sources: sources.map((id) => `${KIND.source}/${id}`),
  }
}

/** An order with each link's sources together: the one ranked first keeps
 * its place and the others follow it, in their order. */
export function keepLinkedTogether(
  order: readonly string[],
  links: readonly SourceLink[]
): string[] {
  let out = [...order]
  for (const link of links) {
    const members = out.filter((id) => link.sources.includes(id))
    if (members.length < 2) continue
    const [head, ...rest] = members
    out = out.filter((id) => !rest.includes(id))
    out.splice(out.indexOf(head) + 1, 0, ...rest)
  }
  return out
}

/** The links a source is part of. */
export function linksOf(
  id: string,
  links: readonly SourceLink[]
): SourceLink[] {
  return links.filter((l) => l.sources.includes(id))
}
