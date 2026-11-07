/** The web pages a finished reply links, gathered under it as numbered
 * source pills, in the order the reply first cites each: the references a
 * reader opens to check an answer. A reply that links no page has none. */

import { useId } from "react"

import { SourcePill } from "@/components/agent/tool-views"
import { replyLinks } from "@/lib/chat-text"

export function ReplySources({ text }: { text: string }) {
  const heading = useId()
  const links = replyLinks(text)
  if (links.length === 0) return null
  return (
    <div className="mt-2.5 flex flex-col gap-1.5">
      {/* The list is named by its visible heading, so a screen reader says
          "Sources" once. */}
      <span id={heading} className="text-[11.5px] font-medium text-faint">
        Sources
      </span>
      <ol aria-labelledby={heading} className="flex flex-wrap gap-1.5">
        {links.map((link, i) => (
          <li key={link.href} className="max-w-full min-w-0">
            <SourcePill url={link.href} title={link.text} index={i + 1} />
          </li>
        ))}
      </ol>
    </div>
  )
}
