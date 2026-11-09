/** A conversation block's reads and writes, through the property names a
 * view gives: one page of messages newest first under the chat, their
 * attachments in one read, the requests written here and the ones a gate
 * holds for it; each message read into its marks; and the request bodies
 * the composer writes, with what may be cancelled or sent again. */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"
import { RECORD_PATCH_REQUEST_KIND } from "@/lib/agent-grants"
import {
  askAgain,
  attachmentsParams,
  canCancel,
  canRetry,
  conversationOrder,
  heldFor,
  heldParams,
  messagesParams,
  messageViewOf,
  newestMessage,
  offerOf,
  reactionGroups,
  requestProperties,
  requestsParams,
  sendNeedsConfirm,
} from "@/lib/conversation"
import type { ComposeSpec, ConversationBlock } from "@/lib/record-view"

const CHAT = "ada.example.com/chat/chat"
const MESSAGE = "ada.example.com/chat/message"
const USER = "ada.example.com/chat/user"
const ACTION = "ada.example.com/chat/action"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  more: Partial<SubstrateRecord> = {}
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 3,
    createdAt: "2026-11-09T10:00:00Z",
    updatedAt: "2026-11-09T10:00:00Z",
    ...more,
  }
}

const COMPOSE: ComposeSpec = {
  kind: ACTION,
  via: "chat",
  verb: "action",
  send: "send",
  react: "react",
  markRead: "markread",
  text: "text",
  replyTo: "replyTo",
  target: "target",
  reaction: "reactionKey",
  state: "state",
  cancel: "cancelRequestedAt",
  cancelIn: ["queued"],
  retryIn: ["failed", "uncertain"],
  error: "errorMessage",
  confirm: "allowNewChat",
  peer: "otherUser",
  peerKnown: "isContact",
}

const BLOCK: ConversationBlock = {
  type: "conversation",
  kind: MESSAGE,
  via: "chat",
  time: "sentAt",
  sender: "sender",
  mine: "fromOwner",
  text: "text",
  replyTo: "replyTo",
  edited: "editedAt",
  deleted: "deleted",
  status: "sendStatus",
  reactions: "reactions",
  attachments: "ada.example.com/chat/attachment",
  attachmentVia: "message",
  attachmentFile: "file",
  readUpTo: "readUpTo",
  limit: 50,
  compose: COMPOSE,
}

const THE_CHAT = record(CHAT, "c1", { otherUser: { ref: `${USER}/u1` } })

describe("the reads", () => {
  it("reads one page of the chat's messages newest first, with their senders and answers", () => {
    expect(messagesParams(BLOCK, THE_CHAT)).toEqual({
      kinds: [MESSAGE],
      first: 50,
      orderBy: "sentAt:desc",
      filter: { properties: { chat: { eq: `${CHAT}/c1` } } },
      expand: ["sender", "replyTo"],
    })
    expect(messagesParams(BLOCK, THE_CHAT, "older")).toMatchObject({
      after: "older",
    })
    expect(
      messagesParams(
        { ...BLOCK, sender: undefined, replyTo: undefined },
        THE_CHAT
      )
    ).not.toHaveProperty("expand")
  })

  it("reads the shown messages' attachments in one read, and none without a kind", () => {
    expect(
      attachmentsParams(BLOCK, [`${MESSAGE}/m1`, `${MESSAGE}/m2`])
    ).toEqual({
      kinds: ["ada.example.com/chat/attachment"],
      first: 200,
      filter: {
        properties: { message: { in: [`${MESSAGE}/m1`, `${MESSAGE}/m2`] } },
      },
    })
    expect(attachmentsParams(BLOCK, [])).toBeUndefined()
    expect(
      attachmentsParams({ ...BLOCK, attachments: undefined }, [`${MESSAGE}/m1`])
    ).toBeUndefined()
  })

  it("reads the requests written here, and the held ones of the request kind", () => {
    expect(requestsParams(COMPOSE, THE_CHAT)).toEqual({
      kinds: [ACTION],
      first: 10,
      orderBy: "createdAt:desc",
      filter: { properties: { chat: { eq: `${CHAT}/c1` } } },
    })
    expect(heldParams(COMPOSE)).toMatchObject({
      kinds: [RECORD_PATCH_REQUEST_KIND],
      filter: {
        properties: {
          targetKind: { eq: ACTION },
          decision: { eq: "proposed" },
        },
      },
    })
  })

  it("keeps the held requests whose diff names this chat", () => {
    const held = (id: string, chat: string) =>
      record(RECORD_PATCH_REQUEST_KIND, id, {
        op: "create",
        targetKind: ACTION,
        targetId: id,
        diff: { properties: { action: "send", chat, text: "hi" } },
      })
    const mine = held("h1", `${CHAT}/c1`)
    const theirs = held("h2", `${CHAT}/c2`)
    const found = heldFor(COMPOSE, THE_CHAT, [mine, theirs])
    expect(found.map((h) => h.request.id)).toEqual(["h1"])
    expect(found[0].properties).toMatchObject({ action: "send", text: "hi" })
  })
})

describe("the messages", () => {
  it("reads a message through the view's names", () => {
    const m = messageViewOf(
      BLOCK,
      record(MESSAGE, "m1", {
        sentAt: "2026-11-09T09:59:58Z",
        sender: { ref: `${USER}/u1` },
        fromOwner: false,
        text: "see you at 7",
        replyTo: { ref: `${MESSAGE}/m0` },
        editedAt: "2026-11-09T10:01:00Z",
        sendStatus: "sent",
        reactions: [
          { key: "👍", user: { ref: `${USER}/u1` } },
          { key: "👍", user: { ref: `${USER}/u2` } },
          { key: "❤️", user: { ref: `${USER}/u2` } },
          { user: { ref: `${USER}/u3` } },
        ],
      })
    )
    expect(m).toMatchObject({
      path: `${MESSAGE}/m1`,
      at: "2026-11-09T09:59:58Z",
      sender: `${USER}/u1`,
      mine: false,
      text: "see you at 7",
      replyTo: `${MESSAGE}/m0`,
      edited: "2026-11-09T10:01:00Z",
      status: "sent",
    })
    expect(m.reactions).toEqual([
      { key: "👍", users: [`${USER}/u1`, `${USER}/u2`], count: 2 },
      { key: "❤️", users: [`${USER}/u2`], count: 1 },
    ])
    expect(m).not.toHaveProperty("deleted")
  })

  it("marks a withdrawn message by any value, and an edit count as edited", () => {
    const m = messageViewOf(
      { ...BLOCK, edited: "editCount" },
      record(MESSAGE, "m2", { deleted: "everyone", editCount: 2 })
    )
    expect(m.deleted).toBe("everyone")
    expect(m.edited).toBe(true)
    expect(
      messageViewOf(
        { ...BLOCK, edited: "editCount" },
        record(MESSAGE, "m3", { editCount: 0 })
      )
    ).not.toHaveProperty("edited")
    expect(reactionGroups("not a list")).toEqual([])
  })

  it("orders pages read newest first oldest first, each message once", () => {
    const at = (id: string, sentAt: string) => record(MESSAGE, id, { sentAt })
    const newer = [
      at("m3", "2026-11-09T10:03:00Z"),
      at("m2", "2026-11-09T10:02:00Z"),
    ]
    const older = [
      at("m2", "2026-11-09T10:02:00Z"),
      at("m1", "2026-11-09T10:01:00Z"),
    ]
    const order = conversationOrder(BLOCK, [newer, older])
    expect(order.map((m) => m.record.id)).toEqual(["m1", "m2", "m3"])
    expect(newestMessage(order)?.record.id).toBe("m3")
    expect(newestMessage([])).toBeUndefined()
  })
})

describe("what the composer writes", () => {
  it("writes a send, a reply, a reaction and a read mark in the view's names", () => {
    expect(
      requestProperties(COMPOSE, THE_CHAT, {
        offer: "send",
        text: "  see you at 7 ",
        replyTo: `${MESSAGE}/m1`,
      })
    ).toEqual({
      action: "send",
      chat: `${CHAT}/c1`,
      text: "see you at 7",
      replyTo: `${MESSAGE}/m1`,
    })
    expect(
      requestProperties(COMPOSE, THE_CHAT, {
        offer: "send",
        text: "hello",
        confirmed: true,
      })
    ).toMatchObject({ allowNewChat: true })
    expect(
      requestProperties(COMPOSE, THE_CHAT, {
        offer: "react",
        target: `${MESSAGE}/m1`,
        reaction: "👍",
      })
    ).toEqual({
      action: "react",
      chat: `${CHAT}/c1`,
      target: `${MESSAGE}/m1`,
      reactionKey: "👍",
    })
    expect(
      requestProperties(COMPOSE, THE_CHAT, {
        offer: "markRead",
        target: `${MESSAGE}/m3`,
      })
    ).toEqual({
      action: "markread",
      chat: `${CHAT}/c1`,
      target: `${MESSAGE}/m3`,
    })
  })

  it("refuses what it cannot write", () => {
    expect(
      requestProperties(COMPOSE, THE_CHAT, { offer: "send", text: "  " })
    ).toBe("Write something to send.")
    expect(
      requestProperties({ ...COMPOSE, react: undefined }, THE_CHAT, {
        offer: "react",
        target: `${MESSAGE}/m1`,
        reaction: "👍",
      })
    ).toBe("This conversation does not offer that.")
    expect(requestProperties(COMPOSE, THE_CHAT, { offer: "markRead" })).toBe(
      "There is nothing to mark."
    )
  })

  it("cancels a queued request once, and sends a failed or uncertain one again as it was", () => {
    const request = (state: string, more: Record<string, unknown> = {}) =>
      record(ACTION, `a-${state}`, {
        action: "send",
        chat: { ref: `${CHAT}/c1` },
        text: "see you at 7",
        replyTo: { ref: `${MESSAGE}/m1` },
        allowNewChat: true,
        state,
        attempt: "x",
        ...more,
      })
    expect(canCancel(COMPOSE, request("queued"))).toBe(true)
    expect(
      canCancel(
        COMPOSE,
        request("queued", { cancelRequestedAt: "2026-11-09T10:00:00Z" })
      )
    ).toBe(false)
    expect(canCancel(COMPOSE, request("sending"))).toBe(false)
    expect(canRetry(COMPOSE, request("failed"))).toBe(true)
    expect(canRetry(COMPOSE, request("uncertain"))).toBe(true)
    expect(canRetry(COMPOSE, request("done"))).toBe(false)
    expect(canRetry({ ...COMPOSE, send: undefined }, request("failed"))).toBe(
      false
    )
    // The same values, never its state, outcome or the confirmation.
    expect(askAgain(COMPOSE, request("failed"))).toEqual({
      offer: "send",
      text: "see you at 7",
      replyTo: `${MESSAGE}/m1`,
    })
    expect(offerOf(COMPOSE, { action: "markread" })).toBe("markRead")
    expect(offerOf(COMPOSE, { action: "edit" })).toBeUndefined()
  })

  it("asks first where the chat's other person is not among the contacts", () => {
    const known = record(USER, "u1", { isContact: true })
    const stranger = record(USER, "u1", { isContact: false })
    expect(sendNeedsConfirm(COMPOSE, THE_CHAT, known)).toBe(false)
    expect(sendNeedsConfirm(COMPOSE, THE_CHAT, stranger)).toBe(true)
    // A peer that cannot be read is not known.
    expect(sendNeedsConfirm(COMPOSE, THE_CHAT, undefined)).toBe(true)
    // A group names no peer: the bridge decides.
    expect(sendNeedsConfirm(COMPOSE, record(CHAT, "g1", {}), undefined)).toBe(
      false
    )
    expect(
      sendNeedsConfirm({ ...COMPOSE, confirm: undefined }, THE_CHAT, stranger)
    ).toBe(false)
  })
})
