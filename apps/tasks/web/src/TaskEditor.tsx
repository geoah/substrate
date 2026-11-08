import {
  BarButton,
  Field,
  Section,
  SelectRow,
  Sheet,
  ToggleRow,
} from "@substrate/react"
import { useState } from "react"
import { draftOf, type TaskDraft } from "./actions"
import {
  civilDay,
  dayDate,
  priorities,
  type Priority,
  type TaskItem,
  type TaskList,
} from "./model"

const noList = "__none__"

function timeOf(d: Date): string {
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`
}

/** One task's form, or a new one's, under the labels its own screen uses.
 * Save writes only what changed, on the version it opened. */
export function TaskEditor(props: {
  open: boolean
  task?: TaskItem
  initial?: Partial<TaskDraft>
  lists: TaskList[]
  today: string
  onClose: () => void
  onSave: (draft: TaskDraft) => Promise<boolean>
}) {
  const [draft, setDraft] = useState<TaskDraft>(() =>
    props.task
      ? draftOf(props.task)
      : {
          name: "",
          description: "",
          url: "",
          priority: "none",
          ...props.initial,
        }
  )
  const [saving, setSaving] = useState(false)
  const set = (patch: Partial<TaskDraft>) =>
    setDraft((d) => ({ ...d, ...patch }))

  const hasDue = !!draft.due
  const hasTime = !!draft.due?.at
  const setDay = (day: string) => {
    if (!day) return
    const at = draft.due?.at
    set({ due: { day, at: at ? at2(day, timeOf(at)) : undefined } })
  }
  const setTime = (time: string) => {
    if (!draft.due || !time) return
    set({ due: { day: draft.due.day, at: at2(draft.due.day, time) } })
  }

  const save = async () => {
    if (!draft.name.trim()) return
    setSaving(true)
    const ok = await props.onSave(draft)
    setSaving(false)
    if (ok) props.onClose()
  }

  return (
    <Sheet
      open={props.open}
      onClose={props.onClose}
      title={props.task ? "Edit Task" : "New Task"}
      actions={
        <BarButton
          prominent
          onClick={save}
          disabled={saving || !draft.name.trim()}
        >
          {props.task ? "Save" : "Add"}
        </BarButton>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <Section>
          <Field
            label="Title"
            value={draft.name}
            onChange={(name) => set({ name })}
            autoFocus={!props.task}
          />
          <Field
            label="Notes"
            value={draft.description}
            onChange={(description) => set({ description })}
            multiline
          />
          <Field
            label="Link"
            value={draft.url}
            onChange={(url) => set({ url })}
            type="url"
            placeholder="https://"
          />
        </Section>
        <Section>
          <SelectRow
            label="List"
            value={draft.list ?? noList}
            options={[
              { value: noList, label: "No List" },
              ...props.lists.map((l) => ({ value: l.id, label: l.name })),
            ]}
            onChange={(v) => set({ list: v === noList ? undefined : v })}
          />
          <SelectRow<Priority>
            label="Priority"
            value={draft.priority}
            options={priorities}
            onChange={(priority) => set({ priority })}
          />
        </Section>
        <Section>
          <ToggleRow
            label="Due Date"
            checked={hasDue}
            onChange={(on) =>
              set({ due: on ? { day: props.today } : undefined })
            }
          />
          {hasDue && (
            <label className="flex min-h-11 items-center gap-3 px-4 py-1.5">
              <span className="text-[17px]">Day</span>
              <input
                type="date"
                className="ml-auto rounded-lg bg-fill px-2 py-1 text-[17px] text-label outline-none"
                value={draft.due!.day}
                onChange={(e) => setDay(e.target.value)}
              />
            </label>
          )}
          {hasDue && (
            <ToggleRow
              label="Time"
              checked={hasTime}
              onChange={(on) =>
                set({
                  due: {
                    day: draft.due!.day,
                    at: on ? at2(draft.due!.day, "09:00") : undefined,
                  },
                })
              }
            />
          )}
          {hasTime && (
            <label className="flex min-h-11 items-center gap-3 px-4 py-1.5">
              <span className="text-[17px]">At</span>
              <input
                type="time"
                className="ml-auto rounded-lg bg-fill px-2 py-1 text-[17px] text-label outline-none"
                value={timeOf(draft.due!.at!)}
                onChange={(e) => setTime(e.target.value)}
              />
            </label>
          )}
        </Section>
        <button type="submit" hidden />
      </form>
    </Sheet>
  )
}

/** A moment on this device's clock: a day and a time of day. */
function at2(day: string, time: string): Date {
  const d = dayDate(day)
  const [h, m] = time.split(":").map(Number)
  d.setHours(h, m, 0, 0)
  // Rounds a day that a clock change skips onto the day it names.
  return civilDay(d) === day ? d : dayDate(day)
}
