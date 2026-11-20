import { ChevronRight } from "lucide-react"
import { formatDate } from "../../lib/format"
import type { Finding } from "../../rules/types"
import { Badge } from "../Badge"

/** One finding as a row that opens what it is about: its title, why in a
 * sentence under it, and at its end the status word over the date it is
 * from. */
export function FindingRow({ finding: f }: { finding: Finding }) {
  return (
    <a
      href={f.href}
      data-section={f.section}
      data-tone={f.tone}
      className="flex min-h-11 items-center gap-3 px-4 py-2.5 hover:bg-fill active:bg-selected"
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-[17px] break-words">{f.title}</span>
        <span className="text-[13px] break-words text-secondary">
          {f.detail}
        </span>
      </span>
      <span className="flex shrink-0 flex-col items-end gap-1">
        <Badge label={f.word} tone={f.tone} />
        {f.since && (
          <time
            dateTime={f.since}
            className="text-[13px] text-secondary tabular-nums"
          >
            {formatDate(f.since)}
          </time>
        )}
      </span>
      <ChevronRight className="size-4 shrink-0 text-tertiary" aria-hidden />
    </a>
  )
}
