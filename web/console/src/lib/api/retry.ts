/** When a read the server refused is asked again, and after how long: the
 * one policy every console query follows (`main.tsx`). Transport trouble
 * and a rate limit earn two more attempts, as does a `503 unavailable`,
 * which the server answers while its aggregate slots are busy and always
 * with a Retry-After. One narrowed to `observed_rederiving` is not asked
 * again: the re-derivation takes minutes, and the page says so with a
 * button to read again. Every other refusal is definitive. */

import { ApiError } from "@/lib/api/types"

/** Attempts after the first. */
const RETRIES = 2

/** react-query's own backoff: a second, two, four, up to thirty. */
const MAX_BACKOFF_MS = 30_000

/** Whether a query that failed `failureCount` times is asked again. */
export function retryRead(failureCount: number, error: unknown): boolean {
  if (error instanceof ApiError) {
    const transient =
      error.code === "network" ||
      error.code === "rate_limited" ||
      (error.code === "unavailable" && error.reason !== "observed_rederiving")
    if (!transient) return false
  }
  return failureCount < RETRIES
}

/** How long before the next attempt: the Retry-After the server named, in
 * milliseconds, else react-query's backoff. */
export function retryDelay(attempt: number, error: unknown): number {
  if (error instanceof ApiError && error.retryAfter && error.retryAfter > 0) {
    return error.retryAfter * 1000
  }
  return Math.min(1000 * 2 ** attempt, MAX_BACKOFF_MS)
}
