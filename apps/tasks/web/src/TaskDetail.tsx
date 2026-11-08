import {
  ActionRow,
  BarButton,
  NavBar,
  Row,
  Section,
  SelectRow,
  cx,
} from "@substrate/react"
import { Link2 } from "lucide-react"
import { useCompleting, useTaskActions } from "./actions"
import { Markdown, safeURL } from "./markdown"
import {
  dueWords,
  fieldLabel,
  fullWords,
  inboxTitle,
  priorities,
  statusWords,
  taskTitle,
  type Priority,
  type TaskItem,
  type TaskList,
} from "./model"
import { StatusMark } from "./TaskRow"

const noList = "__none__"

/** The open task: its name beside its round mark, then each fact once under
 * the label the editor uses, the list and the priority as the choices that
 * change them where they stand, the notes and the link as sections of
 * their own, then what can be done to it, Delete apart. Edit is the bar's. */
export function TaskDetail(props: {
  task: TaskItem
  lists: TaskList[]
  now: Date
  onBack?: () => void
  /** Beside the list, whose large title is the page's heading. */
  beside?: boolean
}) {
  const { task: t, now } = props
  const Title = props.beside ? "h2" : "h1"
  const actions = useTaskActions()
  const completing = useCompleting()
  const pending = completing.get(t.id)
  const due = t.due ? dueWords(t.due, now) : undefined
  const title = taskTitle(t)
  const open = t.status === "open"
  const link = t.url ? safeURL(t.url) : undefined
  const toggle =
    pending !== undefined ? "Undo" : open ? "Mark as Done" : "Reopen"

  return (
    <div className="mx-auto w-full max-w-2xl pb-32">
      <NavBar
        onBack={props.onBack}
        actions={
          <BarButton onClick={() => actions.edit(t.id)} title="Edit (E)">
            Edit
          </BarButton>
        }
      />
      <div className="px-4 pt-2">
        <Section>
          <div className="flex items-center gap-2 py-1.5 pr-1.5 pl-4">
            <Title
              className={cx(
                "min-w-0 flex-1 text-[22px] leading-snug font-semibold break-words select-text",
                open && pending === undefined ? "text-label" : "text-secondary",
                (t.status === "abandoned" || pending !== undefined) &&
                  "line-through"
              )}
            >
              {title}
            </Title>
            <StatusMark
              status={t.status}
              pending={pending}
              label={toggle}
              onToggle={() =>
                open && pending === undefined
                  ? actions.complete(t.id)
                  : actions.reopen(t.id)
              }
            />
          </div>
        </Section>
        <Section>
          <Row
            label={fieldLabel.status}
            value={
              pending !== undefined
                ? "Done, tap Undo to keep it open"
                : statusWords[t.status]
            }
          />
          <SelectRow
            label={fieldLabel.list}
            value={t.list ?? noList}
            options={[
              { value: noList, label: inboxTitle },
              ...props.lists.map((l) => ({ value: l.id, label: l.name })),
            ]}
            onChange={(v) => actions.move(t.id, v === noList ? undefined : v)}
          />
          <Row
            label={fieldLabel.due}
            value={t.due ? fullWords(t.due) : "No due date"}
            valueClassName={
              due?.tone === "overdue" && open ? "text-danger!" : "text-label!"
            }
          />
          <SelectRow<Priority>
            label={fieldLabel.priority}
            value={t.priority}
            options={priorities}
            onChange={(p) => actions.setPriority(t.id, p)}
          />
          {t.status === "done" && t.completedAt && (
            <Row
              label={fieldLabel.completed}
              value={fullWords({ day: "", at: t.completedAt })}
              valueClassName="text-label!"
            />
          )}
        </Section>
        {t.description && (
          <Section header={fieldLabel.notes}>
            <div className="px-4 py-3 text-[17px] leading-relaxed select-text">
              <Markdown text={t.description} />
            </div>
          </Section>
        )}
        {t.url && (
          <Section header={fieldLabel.link}>
            {link ? (
              <a
                href={link}
                target="_blank"
                rel="noreferrer noopener"
                className="flex min-h-11 items-center gap-3 px-4 py-2.5 text-[17px] text-accent-text"
              >
                <Link2 className="size-5 shrink-0" />
                <span className="line-clamp-2 break-all">{t.url}</span>
              </a>
            ) : (
              <p className="px-4 py-2.5 text-[17px] break-all">{t.url}</p>
            )}
          </Section>
        )}
        <Section>
          {pending !== undefined ? (
            <ActionRow onClick={() => actions.reopen(t.id)}>Undo</ActionRow>
          ) : open ? (
            <>
              <ActionRow onClick={() => actions.complete(t.id)}>
                Mark as Done
              </ActionRow>
              <ActionRow onClick={() => actions.abandon(t.id)}>
                Won't Do
              </ActionRow>
            </>
          ) : (
            <ActionRow onClick={() => actions.reopen(t.id)}>Reopen</ActionRow>
          )}
        </Section>
        <Section>
          <ActionRow destructive onClick={() => actions.deleteTask(t.id)}>
            Delete Task
          </ActionRow>
        </Section>
      </div>
    </div>
  )
}
