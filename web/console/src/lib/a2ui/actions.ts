/** A button's A2UI `action`, read at the tap: an `event` goes to the agent
 * with its context and its `userMessage` resolved in the button's scope,
 * and a `functionCall` runs in the renderer. Of the basic catalog's
 * functions only `openUrl` does anything there; the rest answer a value
 * nobody reads. */

import type { AppletAction } from "@/lib/api/types"

import { isRecord } from "./coerce"
import {
  isCall,
  resolve,
  resolveDeep,
  resolveText,
  type EvalContext,
} from "./values"

export type ResolvedAction =
  | { type: "event"; action: AppletAction }
  | { type: "openUrl"; url: unknown }
  | { type: "none"; problem?: string }

export function resolveAction(
  action: unknown,
  ctx: EvalContext,
  sourceComponentId: string
): ResolvedAction {
  if (!isRecord(action)) return { type: "none" }
  const event = action.event
  if (isRecord(event)) {
    if (typeof event.name !== "string" || !event.name)
      return {
        type: "none",
        problem: `${sourceComponentId}: an event without a name`,
      }
    const context = resolveDeep(event.context ?? {}, ctx)
    // What the owner did, in words: read at the tap like the context.
    const userMessage = resolveText(event.userMessage, ctx)
    return {
      type: "event",
      action: {
        name: event.name,
        context: isRecord(context) ? context : {},
        sourceComponentId,
        ...(userMessage && { userMessage }),
      },
    }
  }
  const call = action.functionCall
  if (isCall(call)) {
    if (call["@call"] === "openUrl") {
      const args = isRecord(call.args) ? call.args : {}
      return { type: "openUrl", url: resolve(args.url, ctx) }
    }
    return { type: "none" }
  }
  return {
    type: "none",
    problem: `${sourceComponentId}: an action that is neither an event nor a function call`,
  }
}
