import { cx } from "@substrate/react"
import { ChevronDown, ChevronUp, GripVertical } from "lucide-react"
import { useRef, useState, type PointerEvent } from "react"
import type { Done } from "../../data/sources"
import { moved } from "../../sources/ranks"
import { IconButton } from "../IconButton"

export interface RankItem {
  id: string
  name: string
  /** What the source reports in this group, or why it is here. */
  detail?: string
}

/** One group's sources in rank order, first highest: dragged by the handle
 * on a pointer, moved with Move up and Move down from the keyboard. The new
 * order shows at once and stays while it is written; a write that was not
 * kept puts the order back and says why. */
export function RankList(props: {
  title: string
  items: RankItem[]
  onReorder: (ids: string[]) => Promise<Done>
}) {
  const ids = props.items.map((i) => i.id)
  const key = ids.join("\u0000")
  const [override, setOverride] = useState<{
    order: string[]
    from: string
  } | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  const [drag, setDrag] = useState<{ id: string; to: number } | null>(null)
  const rows = useRef(new Map<string, HTMLLIElement>())

  // The order read back from the repository replaces the one shown once it
  // changes from what it was when the owner moved a source.
  const moving = override?.from === key ? override.order : null

  const byId = new Map(props.items.map((i) => [i.id, i]))
  const base = (moving ?? ids).filter((id) => byId.has(id))
  for (const id of ids) if (!base.includes(id)) base.push(id)
  const shown = drag ? moved(base, base.indexOf(drag.id), drag.to) : base

  const commit = async (next: string[]) => {
    if (next.join("\u0000") === base.join("\u0000")) return
    setOverride({ order: next, from: key })
    setProblem(null)
    const done = await props.onReorder(next)
    if (!done.ok) {
      setOverride(null)
      setProblem(done.words)
    }
  }

  const indexAt = (y: number): number => {
    let i = 0
    for (const id of base) {
      const el = rows.current.get(id)
      if (!el) continue
      const r = el.getBoundingClientRect()
      if (y > r.top + r.height / 2) i++
    }
    return Math.min(i, base.length - 1)
  }

  const onDown = (id: string) => (e: PointerEvent<HTMLSpanElement>) => {
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    setDrag({ id, to: base.indexOf(id) })
  }
  const onMove = (e: PointerEvent<HTMLSpanElement>) => {
    if (!drag) return
    const to = indexAt(e.clientY)
    if (to !== drag.to) setDrag({ ...drag, to })
  }
  const onUp = () => {
    if (!drag) return
    const next = moved(base, base.indexOf(drag.id), drag.to)
    setDrag(null)
    void commit(next)
  }

  return (
    <div className="px-4 py-3">
      <p className="text-[15px] font-semibold">{props.title}</p>
      <ol className="mt-2 flex flex-col gap-1.5" aria-label={props.title}>
        {shown.map((id, i) => {
          const item = byId.get(id)!
          return (
            <li
              key={id}
              ref={(el) => {
                if (el) rows.current.set(id, el)
                else rows.current.delete(id)
              }}
              className={cx(
                "flex min-h-11 items-center gap-2 rounded-[10px] bg-fill py-1.5 pr-1 pl-1",
                drag?.id === id && "ring-2 ring-accent"
              )}
            >
              <span
                aria-hidden
                onPointerDown={onDown(id)}
                onPointerMove={onMove}
                onPointerUp={onUp}
                onPointerCancel={() => setDrag(null)}
                className="flex size-9 shrink-0 cursor-grab touch-none items-center justify-center text-tertiary active:cursor-grabbing"
              >
                <GripVertical className="size-4" />
              </span>
              <span className="w-5 shrink-0 text-[15px] font-semibold text-secondary tabular-nums">
                {i + 1}
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-[15px] break-words">{item.name}</span>
                {item.detail && (
                  <span className="text-[13px] break-words text-secondary">
                    {item.detail}
                  </span>
                )}
              </span>
              <IconButton
                label={`Move ${item.name} up`}
                disabled={i === 0}
                onClick={() => void commit(moved(base, i, i - 1))}
              >
                <ChevronUp className="size-4" aria-hidden />
              </IconButton>
              <IconButton
                label={`Move ${item.name} down`}
                disabled={i === shown.length - 1}
                onClick={() => void commit(moved(base, i, i + 1))}
              >
                <ChevronDown className="size-4" aria-hidden />
              </IconButton>
            </li>
          )
        })}
      </ol>
      {problem && (
        <p role="alert" className="text-danger-ink mt-2 text-[13px]">
          The order was not kept: {problem}
        </p>
      )}
    </div>
  )
}
