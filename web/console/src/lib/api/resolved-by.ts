/** Whose hand resolved a card (decision 0327). A question, a call that
 * waited for approval and a change request each link the actor record of
 * the person who resolved it: `resolvedBy` on an interaction, `decidedBy` on
 * the other two. The link is an address, and a person who is not an admin
 * cannot read the actor behind it, so the page asks the server which of a
 * thread's cards the signed-in person resolved (`me` in a filter) instead of
 * comparing ids (`useResolver`).
 *
 * A record resolved before the link was kept has none, and the page then
 * says nothing about whose hand it was. */

import { queryOptions } from "@tanstack/react-query"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME, LLM_PACKAGE_NAME } from "./http"
import { recordsQueryOptions } from "./records"
import { splitRecordPath } from "@/lib/record-path"

/** The kinds a person resolves, each with the property that links whose
 * hand did, as the declarations name it beside the time they keep. */
export type ResolvedKind = "interaction" | "approval" | "recordpatchrequest"

export const RESOLVER: Record<
  ResolvedKind,
  { package: string; property: "resolvedBy" | "decidedBy" }
> = {
  interaction: { package: LLM_PACKAGE_NAME, property: "resolvedBy" },
  approval: { package: LLM_PACKAGE_NAME, property: "decidedBy" },
  recordpatchrequest: { package: CORE_PACKAGE_NAME, property: "decidedBy" },
}

/** The id of the record a stored reference value names, "" for none. */
export function referencedId(value: unknown): string {
  if (typeof value !== "object" || value === null) return ""
  const ref = (value as { ref?: unknown }).ref
  if (typeof ref !== "string") return ""
  return splitRecordPath(ref)?.id ?? ""
}

/** The cards of one kind that the signed-in person resolved: those of one
 * thread, which one read serves for every card the thread shows, or, for a
 * change request no thread proposed, that one record. */
export function resolvedByMeQueryOptions(
  kind: ResolvedKind,
  at: { thread: string } | { id: string }
) {
  const { package: pkg, property } = RESOLVER[kind]
  return queryOptions({
    ...recordsQueryOptions({
      authority: CORE_AUTHORITY,
      package: pkg,
      name: kind,
      first: 500,
      filter: {
        ...("id" in at && { ids: [at.id] }),
        properties: {
          ...("thread" in at && { thread: { eq: at.thread } }),
          [property]: { eq: "me" },
        },
      },
    }),
    retry: false,
  })
}
