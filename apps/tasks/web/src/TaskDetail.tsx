import {
  ActionRow,
  BarButton,
  Confirm,
  NavBar,
  Row,
  Section,
  SelectRow,
} from "@substrate/react"
import { useState } from "react"
import { useTaskActions } from "./actions"
import {
  dueLong,
  dueWords,
  priorities,
  statusLabel,
  type Priority,
  type TaskItem,
  type TaskList,
} from "./model"
import { TaskEditor } from "./TaskEditor"

const noList = "__none__"

const toneClass = {
  quiet: "text-secondary",
  soon: "text-warn",
  overdue: "text-danger",
} as const

/** The open task: its facts under the labels the editor uses, its list and
 * priority as the choices that change them, and what can be done to it. */
export function TaskDetail(props: {
  task: TaskItem
  lists: TaskList[]
  now: Date
  today: string
  onBack?: () => void
  onDeleted: () => void
}) {
  const { task } = props
  const actions = useTaskActions()
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const due = task.due ? dueWords(task.due, props.now) : undefined

  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <NavBar
        title={task.name}
        onBack={props.onBack}
        actions={<BarButton onClick={() => setEditing(true)}>Edit</BarButton>}
      />
      <div className="px-4 pt-4">
        <Section>
          <div className="px-4 py-3 text-[22px] leading-snug font-bold break-words">
            {task.name}
          </div>
        </Section>
        <Section>
          <Row
            label="Status"
            value={statusLabel[task.status]}
            valueClassName="text-label"
          />
          <SelectRow
            label="List"
            value={task.list ?? noList}
            options={[
              { value: noList, label: "No List" },
              ...props.lists.map((l) => ({ value: l.id, label: l.name })),
            ]}
            onChange={(v) =>
              void actions.move(task, v === noList ? undefined : v)
            }
          />
          {task.due && (
            <Row
              label="Due"
              value={dueLong(task.due)}
              valueClassName={
                task.status === "open" && due ? toneClass[due.tone] : undefined
              }
            />
          )}
          <SelectRow<Priority>
            label="Priority"
            value={task.priority}
            options={priorities}
            onChange={(p) => void actions.setPriority(task, p)}
          />
          {task.description && (
            <Row
              label="Notes"
              value={
                <span className="whitespace-pre-wrap">{task.description}</span>
              }
              valueClassName="text-label"
            />
          )}
          {task.url && (
            <Row
              label="Link"
              value={
                <a
                  href={task.url}
                  target="_blank"
                  rel="noreferrer noopener"
                  className="break-all text-accent-text"
                >
                  {task.url}
                </a>
              }
            />
          )}
        </Section>
        <Section>
          {task.status === "open" ? (
            <>
              <ActionRow onClick={() => void actions.complete(task)}>
                Mark as Done
              </ActionRow>
              <ActionRow onClick={() => void actions.abandon(task)}>
                Won't Do
              </ActionRow>
            </>
          ) : (
            <ActionRow onClick={() => void actions.reopen(task)}>
              Reopen
            </ActionRow>
          )}
          <ActionRow destructive onClick={() => setDeleting(true)}>
            Delete Task
          </ActionRow>
        </Section>
      </div>
      {editing && (
        <TaskEditor
          open
          task={task}
          lists={props.lists}
          today={props.today}
          onClose={() => setEditing(false)}
          onSave={async (d) => !!(await actions.save(task, d))}
        />
      )}
      <Confirm
        open={deleting}
        title={`Delete “${task.name}”?`}
        message="It's deleted on every device."
        confirm="Delete"
        onCancel={() => setDeleting(false)}
        onConfirm={() => {
          setDeleting(false)
          void actions.remove(task)
          props.onDeleted()
        }}
      />
    </div>
  )
}
