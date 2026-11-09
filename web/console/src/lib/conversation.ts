/** A record view's conversation block without React: the reads that list a
 * record's messages, their attachments, the requests its composer wrote and
 * the ones a gate holds, each message read through the property names the
 * view gives, and the bodies the composer writes. The console names no
 * message or request kind here: every kind and property comes from the
 * view, so a chat of any provider that ships one reads the same way. */

import type { ListParams } from "@/lib/api/records"
import { readReference, type Cond, type SubstrateRecord } from "@/lib/api/types"
import { RECORD_PATCH_REQUEST_KIND } from "@/lib/agent-grants"
import { proposedDiff } from "@/lib/changerequests"
import { lowerFirst } from "@/lib/kind-names"
import type { PropSpec } from "@/lib/record-schema"
import type { ComposeSpec, ConversationBlock } from "@/lib/record-view"

/** How many of the newest requests the composer lists. */
export const REQUESTS_SHOWN = 10

/** How many held requests are read to find this record's: a gate holds
 * few at once, and a request names its record only inside its diff. */
const HELD_READ = 100

/** How many attachments one page of messages reads. */
const ATTACHMENTS_READ = 200

function text(v: unknown): string | undefined {
  return typeof v === "string" && v ? v : undefined
}

export function pathOf(r: Pick<SubstrateRecord, "kind" | "id">): string {
  return `${r.kind}/${r.id}`
}

/** The record path a reference property holds: the reference, or the
 * first of a repeated one. */
export function referencePath(
  properties: Record<string, unknown> | undefined,
  name: string | undefined
): string | undefined {
  if (!name || !properties) return undefined
  const v = properties[name]
  return readReference(Array.isArray(v) ? v[0] : v)?.path
}

// ── the reads ───────────────────────────────────────────────────────────────

/** The reference properties a page of messages carries the referents of,
 * one hop: the sender's name and the answered message's text. */
function messageExpand(block: ConversationBlock): string[] {
  return [block.sender, block.replyTo].filter((n): n is string => Boolean(n))
}

/** One page of a record's messages, newest first: the page a conversation
 * shows last. `after` is a previous page's cursor, which reads older ones. */
export function messagesParams(
  block: ConversationBlock,
  record: Pick<SubstrateRecord, "kind" | "id">,
  after?: string
): ListParams {
  const expand = messageExpand(block)
  return {
    kinds: [block.kind],
    first: block.limit,
    orderBy: `${block.time}:desc`,
    filter: { properties: { [block.via]: { eq: pathOf(record) } } },
    ...(expand.length ? { expand } : {}),
    ...(after ? { after } : {}),
  }
}

/** The attachments of the messages shown, in one read. Undefined where
 * the view names no attachment kind or nothing is shown. */
export function attachmentsParams(
  block: ConversationBlock,
  messages: readonly string[]
): ListParams | undefined {
  if (!block.attachments || !block.attachmentVia || !messages.length) {
    return undefined
  }
  const cond: Cond = { in: [...messages] }
  return {
    kinds: [block.attachments],
    first: ATTACHMENTS_READ,
    filter: { properties: { [block.attachmentVia]: cond } },
  }
}

/** The newest requests the composer wrote for this record. */
export function requestsParams(
  compose: ComposeSpec,
  record: Pick<SubstrateRecord, "kind" | "id">
): ListParams {
  return {
    kinds: [compose.kind],
    first: REQUESTS_SHOWN,
    orderBy: "createdAt:desc",
    filter: { properties: { [compose.via]: { eq: pathOf(record) } } },
  }
}

/** The change requests a gate holds that would create a request of the
 * composer's kind. Which record each is for is inside its diff, which no
 * filter reaches, so `heldFor` narrows them. */
export function heldParams(compose: ComposeSpec): ListParams {
  return {
    kinds: [RECORD_PATCH_REQUEST_KIND],
    first: HELD_READ,
    orderBy: "createdAt:desc",
    filter: {
      properties: {
        targetKind: { eq: compose.kind },
        decision: { eq: "proposed" },
      },
    },
  }
}

/** The held requests that would write a request naming this record, with
 * the values each would write. */
export function heldFor(
  compose: ComposeSpec,
  record: Pick<SubstrateRecord, "kind" | "id">,
  held: readonly SubstrateRecord[]
): { request: SubstrateRecord; properties: Record<string, unknown> }[] {
  const own = pathOf(record)
  return held.flatMap((request) => {
    const properties = proposedDiff(request).properties
    return referencePath(properties, compose.via) === own
      ? [{ request, properties }]
      : []
  })
}

// ── the messages ────────────────────────────────────────────────────────────

/** Who reacted with one reaction, in the order the message lists them. */
export interface ReactionGroup {
  key: string
  /** The reacting users' record paths; a reaction naming nobody counts. */
  users: string[]
  count: number
}

/** One message as the conversation draws it, read through the view's
 * property names. */
export interface MessageView {
  path: string
  record: SubstrateRecord
  /** The instant it was sent, as stored. */
  at?: string
  /** The sender's record path. */
  sender?: string
  mine: boolean
  text?: string
  /** The answered message's record path. */
  replyTo?: string
  /** When it was last edited, or `true` where the property is a flag. */
  edited?: string | true
  /** The value that marks it withdrawn, as stored. A withdrawn message
   * keeps what it said: the mark says it is withdrawn, never the absence
   * of its text. */
  deleted?: string
  status?: string
  reactions: ReactionGroup[]
}

/** Reactions grouped by key, in the order each key first appears. Items
 * the view's shape does not fit (no key) are left out. */
export function reactionGroups(raw: unknown): ReactionGroup[] {
  if (!Array.isArray(raw)) return []
  const groups = new Map<string, ReactionGroup>()
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const o = item as Record<string, unknown>
    const key = text(o.key)
    if (!key) continue
    const group = groups.get(key) ?? { key, users: [], count: 0 }
    group.count++
    const user = readReference(o.user)?.path
    if (user && !group.users.includes(user)) group.users.push(user)
    groups.set(key, group)
  }
  return [...groups.values()]
}

export function messageViewOf(
  block: ConversationBlock,
  record: SubstrateRecord
): MessageView {
  const p = record.properties ?? {}
  const edited = block.edited ? p[block.edited] : undefined
  const deleted = block.deleted ? p[block.deleted] : undefined
  return {
    path: pathOf(record),
    record,
    at: text(p[block.time]),
    sender: referencePath(p, block.sender),
    mine: block.mine ? p[block.mine] === true : false,
    text: block.text ? text(p[block.text]) : undefined,
    replyTo: referencePath(p, block.replyTo),
    ...(text(edited)
      ? { edited: text(edited) }
      : edited === true || (typeof edited === "number" && edited > 0)
        ? { edited: true as const }
        : {}),
    ...(deleted !== undefined &&
    deleted !== null &&
    deleted !== false &&
    deleted !== ""
      ? { deleted: typeof deleted === "string" ? deleted : "yes" }
      : {}),
    status: block.status ? text(p[block.status]) : undefined,
    reactions: block.reactions ? reactionGroups(p[block.reactions]) : [],
  }
}

/** How a withdrawn message is marked, in the declaration's own words: the
 * property's label with its value's ("Deleted" and "For everyone" read
 * "Deleted for everyone"), the value's alone where it already starts with
 * the property's, and the property's alone where its values carry none. */
export function deletedMark(spec: PropSpec | undefined, value: string): string {
  const property = spec?.label ?? "Deleted"
  const label = spec?.values?.find((v) => v.value === value)?.label
  if (!label) return property
  if (label.toLowerCase().startsWith(property.toLowerCase())) return label
  return `${property} ${lowerFirst(label)}`
}

function instant(v: string | undefined): number {
  const t = v ? Date.parse(v) : NaN
  return Number.isNaN(t) ? 0 : t
}

/** Pages read newest first, as the conversation reads them: oldest first,
 * each message once, ties broken by path so a reload keeps the order. */
export function conversationOrder(
  block: ConversationBlock,
  pages: readonly (readonly SubstrateRecord[])[]
): MessageView[] {
  const seen = new Map<string, MessageView>()
  for (const page of pages) {
    for (const r of page) {
      const view = messageViewOf(block, r)
      if (!seen.has(view.path)) seen.set(view.path, view)
    }
  }
  return [...seen.values()].sort(
    (a, b) =>
      instant(a.at) - instant(b.at) ||
      (a.path < b.path ? -1 : a.path > b.path ? 1 : 0)
  )
}

/** The newest message, which "Mark as read" marks read up to. The owner's
 * own count: reading up to one's own reply reads everything before it. */
export function newestMessage(
  messages: readonly MessageView[]
): MessageView | undefined {
  return messages[messages.length - 1]
}

// ── what the composer writes ────────────────────────────────────────────────

export type Offer = "send" | "react" | "markRead"

/** One request as the owner asks for it. */
export interface Ask {
  offer: Offer
  text?: string
  /** The answered message's record path. */
  replyTo?: string
  /** The message a reaction or a read mark is about. */
  target?: string
  reaction?: string
  /** The owner confirmed writing to someone outside their contacts. */
  confirmed?: boolean
}

/** The properties of the request record an ask writes, or why it cannot
 * be written: an offer the view does not make, or a value it needs. */
export function requestProperties(
  compose: ComposeSpec,
  record: Pick<SubstrateRecord, "kind" | "id">,
  ask: Ask
): Record<string, unknown> | string {
  const verb = compose[ask.offer]
  if (!verb) return "This conversation does not offer that."
  const out: Record<string, unknown> = {
    [compose.verb]: verb,
    [compose.via]: pathOf(record),
  }
  switch (ask.offer) {
    case "send": {
      const body = ask.text?.trim()
      if (!body || !compose.text) return "Write something to send."
      out[compose.text] = body
      if (ask.replyTo && compose.replyTo) out[compose.replyTo] = ask.replyTo
      if (ask.confirmed && compose.confirm) out[compose.confirm] = true
      break
    }
    case "react":
      if (!ask.target || !compose.target || !compose.reaction || !ask.reaction)
        return "Pick a message and a reaction."
      out[compose.target] = ask.target
      out[compose.reaction] = ask.reaction
      break
    case "markRead":
      if (!ask.target || !compose.target) return "There is nothing to mark."
      out[compose.target] = ask.target
      break
  }
  return out
}

/** Which offer a stored request holds, by its verb's value. */
export function offerOf(
  compose: ComposeSpec,
  request: SubstrateRecord | Record<string, unknown>
): Offer | undefined {
  const p = propertiesOf(request)
  const value = p[compose.verb]
  if (typeof value !== "string") return undefined
  return (["send", "react", "markRead"] as const).find(
    (o) => compose[o] === value
  )
}

function propertiesOf(
  r: SubstrateRecord | Record<string, unknown>
): Record<string, unknown> {
  return "properties" in r &&
    typeof r.properties === "object" &&
    r.properties !== null &&
    "kind" in r
    ? (r.properties as Record<string, unknown>)
    : (r as Record<string, unknown>)
}

/** The ask a stored request was, to send it again as a new request: the
 * same verb and values, never its state, outcome or confirmation, which
 * the owner gives again. */
export function askAgain(
  compose: ComposeSpec,
  request: SubstrateRecord
): Ask | undefined {
  const offer = offerOf(compose, request)
  if (!offer) return undefined
  const p = request.properties ?? {}
  return {
    offer,
    ...(compose.text && text(p[compose.text])
      ? { text: text(p[compose.text]) }
      : {}),
    ...(referencePath(p, compose.replyTo)
      ? { replyTo: referencePath(p, compose.replyTo) }
      : {}),
    ...(referencePath(p, compose.target)
      ? { target: referencePath(p, compose.target) }
      : {}),
    ...(compose.reaction && text(p[compose.reaction])
      ? { reaction: text(p[compose.reaction]) }
      : {}),
  }
}

/** Who asked for a request: the actor that wrote its verb, which nothing
 * but its writer sets. An agent's request a rule held and the owner then
 * added was written by the owner. Read off `propertyMeta`, which only a
 * single record's read carries. */
export function requesterOf(
  compose: ComposeSpec,
  request: SubstrateRecord
): string | undefined {
  return request.propertyMeta?.[compose.verb]?.manager
}

/** The request's state, as stored. */
export function requestState(
  compose: ComposeSpec,
  request: SubstrateRecord
): string | undefined {
  return compose.state ? text(request.properties?.[compose.state]) : undefined
}

/** Whether the owner already asked that the request be cancelled. */
export function cancelAsked(
  compose: ComposeSpec,
  request: SubstrateRecord
): boolean {
  return Boolean(compose.cancel && text(request.properties?.[compose.cancel]))
}

/** Whether the owner may ask that a request be cancelled: the view names
 * the stamp and the states it applies in, the request sits in one, and it
 * was not asked already. */
export function canCancel(
  compose: ComposeSpec,
  request: SubstrateRecord
): boolean {
  const state = requestState(compose, request)
  return Boolean(
    compose.cancel &&
    state &&
    compose.cancelIn.includes(state) &&
    !cancelAsked(compose, request)
  )
}

/** Whether a request may be sent again as a new one: it sits in a state
 * the view names for it, and holds an offer the composer still makes. */
export function canRetry(
  compose: ComposeSpec,
  request: SubstrateRecord
): boolean {
  const state = requestState(compose, request)
  const offer = offerOf(compose, request)
  return Boolean(
    state && compose.retryIn.includes(state) && offer && compose[offer]
  )
}

/** Whether a send asks the owner first: the view names who the record is
 * with and how a contact is told apart, and that person is not known as
 * one. A peer whose record cannot be read is not known. */
export function sendNeedsConfirm(
  compose: ComposeSpec,
  record: Pick<SubstrateRecord, "properties">,
  peer: SubstrateRecord | undefined
): boolean {
  if (!compose.confirm || !compose.peer || !compose.peerKnown) return false
  if (!referencePath(record.properties, compose.peer)) return false
  return peer?.properties?.[compose.peerKnown] !== true
}
