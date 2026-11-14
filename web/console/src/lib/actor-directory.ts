/** The actor records this console has read, by id: what turns the id a change
 * or a manager links into a hand a person can read. The app shell fills it
 * from one cached read (actorsQueryOptions), and every name the console shows
 * for an actor is read from it, so a reader re-renders when it fills
 * (useActorDirectory). */

import { useSyncExternalStore } from "react"

import type { ActorInfo, ActorType } from "@/lib/api/actors"

let byID = new Map<string, ActorInfo>()
let version = 0
const listeners = new Set<() => void>()

/** Replaces what the directory holds. */
export function setActorDirectory(actors: readonly ActorInfo[]): void {
  byID = new Map(actors.map((a) => [a.id, a]))
  version++
  for (const listener of listeners) listener()
}

/** The actor record of an id, undefined while the directory does not hold it. */
export function actorInfo(id: string): ActorInfo | undefined {
  return byID.get(id)
}

/** The id of the actor record of one type that links one identity: an agent's
 * by the agent's identity, a function's by the function's, a package's by
 * the package's. */
export function actorIDOf(type: ActorType, link: string): string | undefined {
  for (const a of byID.values())
    if (a.type === type && a.link === link) return a.id
  return undefined
}

/** Whether an id links the system's actor record: the engine's own hand. */
export function isSystemActor(id: string): boolean {
  return byID.get(id)?.type === "system"
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Re-renders the caller when the directory fills or changes. */
export function useActorDirectory(): number {
  return useSyncExternalStore(
    subscribe,
    () => version,
    () => version
  )
}
