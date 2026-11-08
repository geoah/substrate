import { BarButton, Field, Section, SelectRow, Sheet } from "@substrate/react"
import { CircleAlert } from "lucide-react"
import { useEffect, useEffectEvent, useState } from "react"
import {
  draftOf,
  dueKinds,
  useTaskActions,
  type DueKind,
  type TaskDraft,
} from "./actions"
import {
  atTime,
  civilDay,
  fieldLabel,
  inboxTitle,
  priorities,
  type Priority,
  type TaskItem,
  type TaskList,
} from "./model"
import type { EditorSeed } from "./state"

const noList = "__none__"

const pad = (n: number) => String(n).padStart(2, "0")

/** One task's form, or a new one's (TaskEditor). It keeps the task as it
 * was when it opened, and Save patches the fields the owner changed on
 * that version, so a change made elsewhere meanwhile is never written
 * over. It stays open with the draft until the write is kept; a write that
 * was not is said here, and Save becomes Retry. */
export function TaskEditor(props: {
  seed: EditorSeed
  task?: TaskItem
  lists: TaskList[]
  onClose: () => void
}) {
  const actions = useTaskActions()
  const [seen] = useState(() =>
    props.seed.kind === "task" ? props.task : undefined
  )
  const [draft, setDraft] = useState<TaskDraft>(() =>
    seen
      ? draftOf(seen)
      : {
          name: "",
          notes: "",
          link: "",
          list: props.seed.kind === "new" ? props.seed.list : undefined,
          dueKind: props.seed.kind === "new" && props.seed.day ? "day" : "none",
          day: props.seed.kind === "new" ? props.seed.day : undefined,
          priority: "none",
        }
  )
  const [touched, setTouched] = useState(false)
  const [sending, setSending] = useState(false)
  const [notKept, setNotKept] = useState<string>()

  // A task deleted while it was being edited closes its editor.
  const gone = props.seed.kind === "task" && !props.task
  const close = useEffectEvent(() => props.onClose())
  useEffect(() => {
    if (gone) close()
  }, [gone])

  const set = (patch: Partial<TaskDraft>) => {
    setDraft((d) => ({ ...d, ...patch }))
    setTouched(true)
  }

  const named = draft.name.trim() !== ""
  const save = async () => {
    if (!named || sending) return
    setSending(true)
    let words: string | undefined
    const kept = await actions.save(seen, draft, (w) => (words = w))
    setSending(false)
    if (kept) props.onClose()
    else setNotKept(words ?? "Your server did not take it. Try again.")
  }

  const setDueKind = (kind: DueKind) => {
    const today = civilDay(new Date())
    const day = draft.day ?? today
    if (kind === "none") set({ dueKind: "none" })
    else if (kind === "day") set({ dueKind: "day", day })
    else {
      // A moment on the day chosen: the hour reminders come at.
      const at = draft.at ?? atTime(day, 9, 0)
      set({ dueKind: "time", day: civilDay(at), at })
    }
  }

  const setDay = (day: string) => {
    if (!day) return
    if (draft.dueKind === "time" && draft.at)
      set({ day, at: atTime(day, draft.at.getHours(), draft.at.getMinutes()) })
    else set({ day })
  }

  const setTime = (time: string) => {
    const [h, m] = time.split(":").map(Number)
    if (!draft.day || Number.isNaN(h) || Number.isNaN(m)) return
    set({ at: atTime(draft.day, h, m) })
  }

  const label = notKept ? "Retry" : seen ? "Save" : "Add"
  return (
    <Sheet
      open
      onClose={props.onClose}
      title={seen ? "Edit Task" : "New Task"}
      actions={
        <BarButton
          prominent
          onClick={save}
          disabled={!named || sending}
          title={`${label} (⌘↵)`}
        >
          {label}
        </BarButton>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            void save()
          }
        }}
      >
        {notKept && (
          <div
            role="alert"
            className="mb-5 flex gap-3 rounded-[10px] bg-card px-4 py-3"
          >
            <CircleAlert className="mt-0.5 size-5 shrink-0 text-danger" />
            <div className="min-w-0">
              <p className="text-[17px] font-semibold">A change was not kept</p>
              <p className="text-[15px] text-secondary">{notKept}</p>
            </div>
          </div>
        )}
        <Section
          footer={
            touched && !named ? (
              <span className="text-danger">A task needs a name.</span>
            ) : undefined
          }
        >
          <Field
            label={fieldLabel.name}
            value={draft.name}
            onChange={(name) => set({ name })}
            placeholder="What to do"
            autoFocus={!seen}
          />
          <Field
            label={fieldLabel.notes}
            value={draft.notes}
            onChange={(notes) => set({ notes })}
            placeholder="Details"
            multiline
          />
          <Field
            label={fieldLabel.link}
            value={draft.link}
            onChange={(link) => set({ link })}
            type="url"
            placeholder="https://example.com"
          />
        </Section>
        <Section>
          <SelectRow
            label={fieldLabel.list}
            value={draft.list ?? noList}
            options={[
              { value: noList, label: inboxTitle },
              ...props.lists.map((l) => ({ value: l.id, label: l.name })),
            ]}
            onChange={(v) => set({ list: v === noList ? undefined : v })}
          />
          <SelectRow<DueKind>
            label={fieldLabel.due}
            value={draft.dueKind}
            options={dueKinds}
            onChange={setDueKind}
          />
          {draft.dueKind !== "none" && draft.day && (
            <div className="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-1.5">
              <span className="text-[17px]">
                {draft.dueKind === "day" ? "Day" : "Day and Time"}
              </span>
              <span className="ml-auto flex gap-2">
                <input
                  type="date"
                  aria-label="Day"
                  className="rounded-lg bg-fill px-2.5 py-1.5 text-[17px] text-label outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  value={draft.day}
                  onChange={(e) => setDay(e.target.value)}
                />
                {draft.dueKind === "time" && draft.at && (
                  <input
                    type="time"
                    aria-label="Time"
                    className="rounded-lg bg-fill px-2.5 py-1.5 text-[17px] text-label outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    value={`${pad(draft.at.getHours())}:${pad(draft.at.getMinutes())}`}
                    onChange={(e) => setTime(e.target.value)}
                  />
                )}
              </span>
            </div>
          )}
          <SelectRow<Priority>
            label={fieldLabel.priority}
            value={draft.priority}
            options={priorities}
            onChange={(priority) => set({ priority })}
          />
        </Section>
        <button type="submit" hidden />
      </form>
    </Sheet>
  )
}
