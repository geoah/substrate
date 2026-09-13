/** The minute, as a store: a row's "in 5 minutes" and a list's Today bucket
 * read the clock, and a render may not call `Date.now()` (it is not
 * idempotent). One interval shared by every subscriber, started with the
 * first and stopped with the last, so an idle guest wakes no timer. */

import { useSyncExternalStore } from "react"

const MINUTE = 60_000

const listeners = new Set<() => void>()
let timer: ReturnType<typeof setInterval> | undefined

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  timer ??= setInterval(() => {
    for (const l of [...listeners]) l()
  }, MINUTE)
  return () => {
    listeners.delete(listener)
    if (!listeners.size && timer !== undefined) {
      clearInterval(timer)
      timer = undefined
    }
  }
}

/** The current minute, in epoch milliseconds: a primitive, so two reads
 * inside one minute are one value and React re-renders only on the tick. */
function snapshot(): number {
  return Math.floor(Date.now() / MINUTE) * MINUTE
}

export function useNow(): number {
  return useSyncExternalStore(subscribe, snapshot, snapshot)
}
