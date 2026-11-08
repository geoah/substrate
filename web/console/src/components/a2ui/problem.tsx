import { useEffect } from "react"

import { useSurface } from "./context"

/** Draws nothing and says why, once it has committed: the host's state must
 * not change while a child renders. */
export function Problem({ message }: { message: string }) {
  const { problem } = useSurface()
  useEffect(() => problem(message), [problem, message])
  return null
}
