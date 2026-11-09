import { NavBar, Row, Section, SelectRow, cx } from "@substrate/react"
import { Sparkle } from "lucide-react"
import { useEffect, useState } from "react"
import { usePending, useTaskActions } from "./actions"
import { DescriptionSection, DetailsSection } from "./Details"
import {
  atTime,
  fieldLabel,
  fullWords,
  listTitle,
  priorities,
  timestamp,
  type TaskItem,
} from "./model"
import { writtenWords } from "./present"
import { nameWords, propertyLabel } from "./sources"
import type { TasksModel } from "./state"
import { words } from "./words"

const none = "__none__"

/** A suggestion: its collection and when it was written, its title, the
 * fields the owner may set before accepting it, its description under its
 * declared label, every other property under Details, and Dismiss and
 * Accept docked under it. What is set goes in the accept's own patch. */
export function SuggestionDetail(props: {
  task: TaskItem
  model: TasksModel
  now: Date
  onBack?: () => void
  beside?: boolean
  /** Where it stands in the queue: "1 of 6". */
  place?: string
  /** A phone: the tab bar is under the docked buttons. */
  overTabBar?: boolean
}) {
  const { task: t, model, now } = props
  const Title = props.beside ? "h2" : "h1"
  const actions = useTaskActions()
  const decision = usePending().get(t.id)
  const c = t.item.collection
  const definition = c.declaration.definition
  const writable = !c.viewOnly
  const [edits, setEdits] = useState<Record<string, unknown>>({})
  const set = (prop: string, value: unknown) =>
    setEdits((e) => ({ ...e, [prop]: value }))
  const valueOf = (prop: string) =>
    prop in edits ? edits[prop] : t.item.record.properties[prop]

  const groups = c.group
    ? [...model.groups.values()].filter((l) =>
        l.id.startsWith(`${c.group!.kind}/`)
      )
    : []
  const groupLabel = c.group
    ? propertyLabel(definition, c.group.property)
    : undefined
  const groupValue = c.group
    ? c.group.property in edits
      ? ((edits[c.group.property] as string | null) ?? none)
      : (t.list ?? none)
    : none
  const dayProp = c.dueDay
  const atProp = c.due
  const dayValue =
    (dayProp && (valueOf(dayProp) as string | undefined)) ??
    (atProp && typeof valueOf(atProp) === "string"
      ? (valueOf(atProp) as string).slice(0, 10)
      : undefined) ??
    ""
  /** A day chosen: the day where the collection reads one, and where it
   * reads a moment only, nine on that day; cleared, both. */
  const setDay = (day: string) => {
    if (dayProp) set(dayProp, day || null)
    if (atProp)
      set(
        atProp,
        day && !dayProp ? timestamp(atTime(day, 9, 0)) : day ? undefined : null
      )
  }
  const cleaned = Object.fromEntries(
    Object.entries(edits).filter(([, v]) => v !== undefined)
  )

  const accept = () =>
    actions.act(
      t.id,
      "accept",
      Object.keys(cleaned).length ? { edits: cleaned } : {}
    )
  const canAccept = t.item.actions.accept.length > 0
  const canDismiss = t.item.actions.dismiss.length > 0

  // A accepts it with what was set, D dismisses it, where the focus is not
  // in a field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return
      const target = e.target as HTMLElement | null
      if (
        target &&
        (target.isContentEditable ||
          /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))
      )
        return
      if (
        document.querySelector("[role=dialog],[role=alertdialog],[role=menu]")
      )
        return
      const key = e.key.toLowerCase()
      if (key === "a" && canAccept) {
        e.preventDefault()
        actions.act(
          t.id,
          "accept",
          Object.keys(cleaned).length ? { edits: cleaned } : {}
        )
      } else if (key === "d" && canDismiss) {
        e.preventDefault()
        actions.act(t.id, "dismiss")
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  })

  return (
    <div className="relative mx-auto flex min-h-full w-full max-w-2xl flex-col">
      <NavBar
        onBack={props.onBack}
        backLabel={words.queue.title}
        actions={
          props.place && (
            <span className="inline-flex h-11 items-center rounded-full bg-card/80 px-4 text-[17px] shadow-[0_1px_6px_rgb(0_0_0/0.08)] ring-1 ring-separator/40 backdrop-blur-xl">
              {props.place}
            </span>
          )
        }
      />
      <div className="flex-1 px-4 pt-1 pb-32">
        <p className="flex items-center gap-2 px-1 pb-2 text-[15px] text-secondary">
          <Sparkle
            className="size-3.5 shrink-0 fill-accent text-accent"
            aria-hidden
          />
          <span>
            {words.suggestion.heading(
              c.label.plural,
              writtenWords(t.item.createdAt, now, true)
            )}
          </span>
        </p>
        <Section>
          <div className="flex items-start gap-3 px-4 py-3">
            <Sparkle
              className="mt-1.5 size-5 shrink-0 fill-accent text-accent"
              aria-hidden
            />
            <Title className="min-w-0 flex-1 text-[22px] leading-snug font-semibold break-words select-text">
              {t.name || t.item.title}
            </Title>
          </div>
        </Section>
        {writable && (c.group || dayProp || atProp || c.priority || t.own) && (
          <Section>
            {c.group && (
              <SelectRow
                label={groupLabel}
                value={groupValue}
                options={[
                  {
                    value: none,
                    label: words.lists.none(groupLabel ?? "list"),
                  },
                  ...groups.map((l) => ({ value: l.id, label: listTitle(l) })),
                ]}
                onChange={(v) => set(c.group!.property, v === none ? null : v)}
              />
            )}
            {(dayProp || atProp) && (
              <label className="flex min-h-11 items-center gap-3 px-4 py-1.5">
                <span className="text-[17px]">{fieldLabel.due}</span>
                <input
                  type="date"
                  aria-label={fieldLabel.due}
                  value={dayValue}
                  onChange={(e) => setDay(e.target.value)}
                  className="ml-auto rounded-lg bg-fill px-2.5 py-1.5 text-[17px] text-label outline-none focus-visible:ring-2 focus-visible:ring-accent"
                />
              </label>
            )}
            {c.priority && (
              <SelectRow
                label={propertyLabel(definition, c.priority)}
                value={(valueOf(c.priority) as string | undefined) ?? none}
                options={[
                  ...(t.own
                    ? []
                    : [{ value: none, label: words.collection.none }]),
                  ...(t.own
                    ? priorities
                    : c.priorities.map((v) => ({
                        value: v,
                        label: nameWords(v),
                      }))),
                ]}
                onChange={(v) => set(c.priority!, v === none ? null : v)}
              />
            )}
          </Section>
        )}
        {!writable && (
          <Section>
            {c.group && (
              <Row
                label={groupLabel}
                value={
                  t.list
                    ? listTitle(model.list(t.list) ?? { name: "" })
                    : words.lists.none(groupLabel ?? "list")
                }
              />
            )}
            {(dayProp || atProp) && (
              <Row
                label={fieldLabel.due}
                value={t.due ? fullWords(t.due) : "None"}
              />
            )}
          </Section>
        )}
        <DescriptionSection task={t} />
        <DetailsSection task={t} model={model} />
      </div>
      {(canAccept || canDismiss || decision) && (
        <div
          className={cx(
            "sticky z-10 flex gap-3 bg-gradient-to-t from-grouped via-grouped/95 to-transparent px-4 pt-6",
            props.overTabBar
              ? "bottom-[max(5.25rem,calc(env(safe-area-inset-bottom)+4.75rem))] pb-2"
              : "bottom-0 pb-[max(1rem,env(safe-area-inset-bottom))]"
          )}
        >
          {decision ? (
            <>
              <span className="flex h-[52px] flex-1 items-center justify-center text-[17px] text-secondary">
                {words.undo[decision.action]}
              </span>
              <button
                type="button"
                onClick={() => actions.reopen(t.id)}
                className="h-[52px] flex-1 rounded-full bg-fill text-[17px] font-semibold active:bg-selected"
              >
                {words.undo.action}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                disabled={!canDismiss}
                onClick={() => actions.act(t.id, "dismiss")}
                title={`${words.suggestion.dismiss} (D)`}
                className="h-[52px] flex-1 rounded-full bg-fill text-[17px] font-semibold active:bg-selected disabled:opacity-40"
              >
                {words.suggestion.dismiss}
              </button>
              <button
                type="button"
                disabled={!canAccept}
                onClick={accept}
                title={`${words.suggestion.accept} (A)`}
                className={cx(
                  "h-[52px] flex-1 rounded-full bg-accent text-[17px] font-semibold text-white active:opacity-80 disabled:opacity-40"
                )}
              >
                {words.suggestion.accept}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  )
}
