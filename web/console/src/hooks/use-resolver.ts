/** Who resolved a card (decision 0327): the signed-in person, somebody else
 * by name where the reader may read actor records, or somebody else and no
 * more. The server says which cards are the reader's own (`me` in a filter),
 * so the page never needs its person's actor id. */

import { useQuery } from "@tanstack/react-query"

import { REPLY_IN_WORDS_REVISION } from "@/lib/api/agents"
import { discoveryQueryOptions, hasFeature } from "@/lib/api/discovery"
import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "@/lib/api/http"
import { recordQueryOptions } from "@/lib/api/records"
import {
  referencedId,
  RESOLVER,
  resolvedByMeQueryOptions,
  type ResolvedKind,
} from "@/lib/api/resolved-by"
import type { SubstrateRecord } from "@/lib/api/types"

/** Who resolved a card: the signed-in person, or somebody else, by name
 * where the reader may read actors. */
export type Resolver = { who: "you" } | { who: "other"; name?: string }

/** Who resolved `record`, a record of `kind`: undefined while it waits,
 * where it links nobody (it was resolved before the link was kept, or a
 * policy rule decided it), and until the server has answered. */
export function useResolver(
  kind: ResolvedKind,
  record: SubstrateRecord | undefined
): Resolver | undefined {
  const actor = referencedId(record?.properties[RESOLVER[kind].property])
  const thread = referencedId(record?.properties.thread)
  const discovery = useQuery(discoveryQueryOptions)
  // A server from before the link does not declare the property, and
  // refuses a filter on it: nothing is asked of one.
  const asks =
    Boolean(actor && record) &&
    Boolean(discovery.data) &&
    hasFeature(discovery.data, "agents", REPLY_IN_WORDS_REVISION)
  const mine = useQuery({
    ...resolvedByMeQueryOptions(
      kind,
      thread ? { thread } : { id: record?.id ?? "" }
    ),
    enabled: asks,
  })
  // A record the answer holds is the reader's. One it does not hold is
  // somebody else's only where the answer is whole: past its first page
  // nothing is concluded, and the card says only how it ended.
  const found = mine.data?.records.some((r) => r.id === record?.id)
  const isMine = found
    ? true
    : mine.data && !mine.data.cursor
      ? false
      : undefined
  // Somebody else's actor, where the reader's roles read actors: an admin
  // sees the name, and anyone else is told only that it was not them.
  const other = useQuery({
    ...recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, "actor", actor),
    enabled: asks && isMine === false,
    retry: false,
  })
  if (!asks || isMine === undefined) return undefined
  if (isMine) return { who: "you" }
  const name = other.data?.properties.name
  return {
    who: "other",
    ...(typeof name === "string" && name ? { name } : {}),
  }
}

/** The words for who: "You", the person's name, or that it was somebody
 * else. */
export function resolverWords(resolver: Resolver): string {
  if (resolver.who === "you") return "You"
  return resolver.name ?? "Someone else"
}
