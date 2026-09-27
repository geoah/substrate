import { useContext } from "react"
import { QueryClientContext } from "@tanstack/react-query"

/** Whether a QueryClient is mounted above. An identity mark renders anywhere
 * — inside a component test with no client, on the sign-in pages — and only
 * reaches for data (a title, a hover card's facts) where it can. */
export function useHasQueryClient(): boolean {
  return useContext(QueryClientContext) !== undefined
}
