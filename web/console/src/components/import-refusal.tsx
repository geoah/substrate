import type { ReactNode } from "react"

import { importFailureLines } from "@/lib/bundles"

/** The server's refusal of an import, an install or an upgrade, one problem
 * per line. Admission answers with every problem at once, and the whole
 * list is what the reader acts on: joined into one paragraph, a refusal of
 * four problems read as its first clause alone (issue 616). A single
 * problem, or an envelope with none, is the one sentence it is. */
export function ImportRefusal({ error }: { error: unknown }): ReactNode {
  const lines = importFailureLines(error)
  if (lines.length === 1) {
    return <span className="break-words">{lines[0]}</span>
  }
  return (
    <ul className="list-disc space-y-1 pl-4">
      {lines.map((line) => (
        <li key={line} className="break-words">
          {line}
        </li>
      ))}
    </ul>
  )
}
